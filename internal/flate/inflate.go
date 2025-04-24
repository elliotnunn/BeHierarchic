// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flate implements the DEFLATE compressed data format, described in
// RFC 1951.  The gzip and zlib packages implement access to DEFLATE-based file
// formats.
package flate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"sync"
)

const (
	maxCodeLen = 16 // max length of Huffman code
	// The next three numbers come from the RFC section 3.2.7, with the
	// additional proviso in section 3.2.5 which implies that distance codes
	// 30 and 31 should never occur in compressed data.
	maxNumLit      = 286
	maxNumDist     = 30
	numCodes       = 19      // number of codes in Huffman meta-code
	maxMatchOffset = 1 << 15 // The largest match offset
	endBlockMarker = 256
)

// Initialize the fixedHuffmanDecoder only once upon first use.
var fixedOnce sync.Once
var fixedHuffmanDecoder huffmanDecoder

// The data structure for decoding Huffman tables is based on that of
// zlib. There is a lookup table of a fixed bit width (huffmanChunkBits),
// For codes smaller than the table width, there are multiple entries
// (each combination of trailing bits has the same value). For codes
// larger than the table width, the table contains a link to an overflow
// table. The width of each entry in the link table is the maximum code
// size minus the chunk width.
//
// Note that you can do a lookup in the table even without all bits
// filled. Since the extra bits are zero, and the DEFLATE Huffman codes
// have the property that shorter codes come before longer ones, the
// bit length estimate in the result is a lower bound on the actual
// number of bits.
//
// See the following:
//	https://github.com/madler/zlib/raw/master/doc/algorithm.txt

// chunk & 15 is number of bits
// chunk >> 4 is value, including table link

const (
	huffmanChunkBits  = 9
	huffmanNumChunks  = 1 << huffmanChunkBits
	huffmanCountMask  = 15
	huffmanValueShift = 4
)

func readAtLeast(zip io.ReaderAt, zipsize int64, rp *resumePoint, minsize int) (resumePoint, error) {
	fixedHuffmanDecoderInit()

	if len(rp.big) != 0 && len(rp.big) != maxMatchOffset {
		panic("this resumepoint is populated, why not just use it?")
	}

	if (len(rp.big) == 0) != (rp.woffset == 0) || (len(rp.big) == 0) != (rp.roffset == 0 && rp.nb == 0) {
		panic("discrepancy about whether this is the first block or not")
	}

	f := decompressor{
		r:  bufio.NewReader(io.NewSectionReader(zip, rp.roffset, zipsize-rp.roffset)),
		rp: *rp,
	}
	if len(f.rp.big) == 0 {
		f.rp.big = make([]byte, maxMatchOffset) // zero out the dictionary
	}

	var err error
	for err == nil && len(f.rp.big) < maxMatchOffset+minsize {
		err = f.nextBlock()
	}

	rp.big = f.rp.big // copy this slice back where it came from
	nrp := f.rp
	nrp.big = make([]byte, maxMatchOffset)
	nrp.woffset += int64(len(f.rp.big) - maxMatchOffset)
	copy(nrp.big, f.rp.big[len(f.rp.big)-maxMatchOffset:])
	return nrp, err // which might be quite a serious error
}

type huffmanDecoder struct {
	min      int                      // the minimum code length
	chunks   [huffmanNumChunks]uint32 // chunks as described above
	links    [][]uint32               // overflow links
	linkMask uint32                   // mask the width of the link table
}

// Initialize Huffman decoding tables from array of code lengths.
// Following this function, h is guaranteed to be initialized into a complete
// tree (i.e., neither over-subscribed nor under-subscribed). The exception is a
// degenerate case where the tree has only a single symbol with length 1. Empty
// trees are permitted.
func (h *huffmanDecoder) init(lengths []int) bool {
	// Sanity enables additional runtime tests during Huffman
	// table construction. It's intended to be used during
	// development to supplement the currently ad-hoc unit tests.
	const sanity = false

	if h.min != 0 {
		*h = huffmanDecoder{}
	}

	// Count number of codes of each length,
	// compute min and max length.
	var count [maxCodeLen]int
	var min, max int
	for _, n := range lengths {
		if n == 0 {
			continue
		}
		if min == 0 || n < min {
			min = n
		}
		if n > max {
			max = n
		}
		count[n]++
	}

	// Empty tree. The decompressor.huffSym function will fail later if the tree
	// is used. Technically, an empty tree is only valid for the HDIST tree and
	// not the HCLEN and HLIT tree. However, a stream with an empty HCLEN tree
	// is guaranteed to fail since it will attempt to use the tree to decode the
	// codes for the HLIT and HDIST trees. Similarly, an empty HLIT tree is
	// guaranteed to fail later since the compressed data section must be
	// composed of at least one symbol (the end-of-block marker).
	if max == 0 {
		return true
	}

	code := 0
	var nextcode [maxCodeLen]int
	for i := min; i <= max; i++ {
		code <<= 1
		nextcode[i] = code
		code += count[i]
	}

	// Check that the coding is complete (i.e., that we've
	// assigned all 2-to-the-max possible bit sequences).
	// Exception: To be compatible with zlib, we also need to
	// accept degenerate single-code codings. See also
	// TestDegenerateHuffmanCoding.
	if code != 1<<uint(max) && !(code == 1 && max == 1) {
		return false
	}

	h.min = min
	if max > huffmanChunkBits {
		numLinks := 1 << (uint(max) - huffmanChunkBits)
		h.linkMask = uint32(numLinks - 1)

		// create link tables
		link := nextcode[huffmanChunkBits+1] >> 1
		h.links = make([][]uint32, huffmanNumChunks-link)
		for j := uint(link); j < huffmanNumChunks; j++ {
			reverse := int(bits.Reverse16(uint16(j)))
			reverse >>= uint(16 - huffmanChunkBits)
			off := j - uint(link)
			if sanity && h.chunks[reverse] != 0 {
				panic("impossible: overwriting existing chunk")
			}
			h.chunks[reverse] = uint32(off<<huffmanValueShift | (huffmanChunkBits + 1))
			h.links[off] = make([]uint32, numLinks)
		}
	}

	for i, n := range lengths {
		if n == 0 {
			continue
		}
		code := nextcode[n]
		nextcode[n]++
		chunk := uint32(i<<huffmanValueShift | n)
		reverse := int(bits.Reverse16(uint16(code)))
		reverse >>= uint(16 - n)
		if n <= huffmanChunkBits {
			for off := reverse; off < len(h.chunks); off += 1 << uint(n) {
				// We should never need to overwrite
				// an existing chunk. Also, 0 is
				// never a valid chunk, because the
				// lower 4 "count" bits should be
				// between 1 and 15.
				if sanity && h.chunks[off] != 0 {
					panic("impossible: overwriting existing chunk")
				}
				h.chunks[off] = chunk
			}
		} else {
			j := reverse & (huffmanNumChunks - 1)
			if sanity && h.chunks[j]&huffmanCountMask != huffmanChunkBits+1 {
				// Longer codes should have been
				// associated with a link table above.
				panic("impossible: not an indirect chunk")
			}
			value := h.chunks[j] >> huffmanValueShift
			linktab := h.links[value]
			reverse >>= huffmanChunkBits
			for off := reverse; off < len(linktab); off += 1 << uint(n-huffmanChunkBits) {
				if sanity && linktab[off] != 0 {
					panic("impossible: overwriting existing chunk")
				}
				linktab[off] = chunk
			}
		}
	}

	if sanity {
		// Above we've sanity checked that we never overwrote
		// an existing entry. Here we additionally check that
		// we filled the tables completely.
		for i, chunk := range h.chunks {
			if chunk == 0 {
				// As an exception, in the degenerate
				// single-code case, we allow odd
				// chunks to be missing.
				if code == 1 && i%2 == 1 {
					continue
				}
				panic("impossible: missing chunk")
			}
		}
		for _, linktab := range h.links {
			for _, chunk := range linktab {
				if chunk == 0 {
					panic("impossible: missing chunk")
				}
			}
		}
	}

	return true
}

// The actual read interface needed by [NewReader].
// If the passed in io.Reader does not also have ReadByte,
// the [NewReader] will introduce its own buffering.
type Reader interface {
	io.Reader
	io.ByteReader
}

type resumePoint struct {
	big     []byte
	roffset int64
	b       uint32
	nb      uint
	woffset int64
}

// Decompress state.
type decompressor struct {
	// Input source (must be seek-ed to "DEFLATE base"+rp.roffset)
	r Reader
	// State required for mid-DEFLATE resumption
	rp resumePoint
}

func (rp *resumePoint) String() string {
	return fmt.Sprintf("big=%#x bytes, roffset=%#x, b=%#x, nb=%d, woffset=%#x",
		len(rp.big), rp.roffset, rp.b, rp.nb, rp.woffset)
}

func (f *decompressor) nextBlock() (ret error) {
	defer func() {
		if r := recover(); r != nil {
			ret = errors.New("corrupt DEFLATE")
		}
	}()

	for f.rp.nb < 1+2 {
		f.moreBits()
	}
	final := f.rp.b&1 == 1
	f.rp.b >>= 1
	typ := f.rp.b & 3
	f.rp.b >>= 2
	f.rp.nb -= 1 + 2

	switch typ {
	case 0:
		f.dataBlock()
	case 1:
		// compressed, fixed Huffman tables
		f.huffmanBlock(&fixedHuffmanDecoder, nil)
	case 2:
		// compressed, dynamic Huffman tables
		var h1, h2 huffmanDecoder
		f.readHuffman(&h1, &h2)
		f.huffmanBlock(&h1, &h2)
	default:
		// 3 is reserved.
		panic("corrupt DEFLATE")
	}

	if final {
		return io.EOF
	}
	return nil
}

// RFC 1951 section 3.2.7.
// Compression with dynamic Huffman codes

var codeOrder = [...]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

func (f *decompressor) readHuffman(h1, h2 *huffmanDecoder) {
	var bits [maxNumLit + maxNumDist]int
	var codebits [numCodes]int

	// HLIT[5], HDIST[5], HCLEN[4].
	for f.rp.nb < 5+5+4 {
		f.moreBits()
	}
	nlit := int(f.rp.b&0x1F) + 257
	if nlit > maxNumLit {
		panic("corrupt DEFLATE")
	}
	f.rp.b >>= 5
	ndist := int(f.rp.b&0x1F) + 1
	if ndist > maxNumDist {
		panic("corrupt DEFLATE")
	}
	f.rp.b >>= 5
	nclen := int(f.rp.b&0xF) + 4
	// numCodes is 19, so nclen is always valid.
	f.rp.b >>= 4
	f.rp.nb -= 5 + 5 + 4

	// (HCLEN+4)*3 bits: code lengths in the magic codeOrder order.
	for i := 0; i < nclen; i++ {
		for f.rp.nb < 3 {
			f.moreBits()
		}
		codebits[codeOrder[i]] = int(f.rp.b & 0x7)
		f.rp.b >>= 3
		f.rp.nb -= 3
	}
	for i := nclen; i < len(codeOrder); i++ {
		codebits[codeOrder[i]] = 0
	}
	if !h1.init(codebits[0:]) {
		panic("corrupt DEFLATE")
	}

	// HLIT + 257 code lengths, HDIST + 1 code lengths,
	// using the code length Huffman code.
	for i, n := 0, nlit+ndist; i < n; {
		x := f.huffSym(h1)
		if x < 16 {
			// Actual length.
			bits[i] = x
			i++
			continue
		}
		// Repeat previous length or zero.
		var rep int
		var nb uint
		var b int
		switch x {
		default:
			panic("unexpected length code")
		case 16:
			rep = 3
			nb = 2
			if i == 0 {
				panic("corrupt DEFLATE")
			}
			b = bits[i-1]
		case 17:
			rep = 3
			nb = 3
			b = 0
		case 18:
			rep = 11
			nb = 7
			b = 0
		}
		for f.rp.nb < nb {
			f.moreBits()
		}
		rep += int(f.rp.b & uint32(1<<nb-1))
		f.rp.b >>= nb
		f.rp.nb -= nb
		if i+rep > n {
			panic("corrupt DEFLATE")
		}
		for j := 0; j < rep; j++ {
			bits[i] = b
			i++
		}
	}

	if !h1.init(bits[0:nlit]) || !h2.init(bits[nlit:nlit+ndist]) {
		panic("corrupt DEFLATE")
	}

	// As an optimization, we can initialize the min bits to read at a time
	// for the HLIT tree to the length of the EOB marker since we know that
	// every block must terminate with one. This preserves the property that
	// we never read any extra bytes after the end of the DEFLATE stream.
	if h1.min < bits[endBlockMarker] {
		h1.min = bits[endBlockMarker]
	}
}

// Decode a single Huffman block from f.
// hl and hd are the Huffman states for the lit/length values
// and the distance values, respectively. If hd == nil, using the
// fixed distance encoding associated with fixed Huffman blocks.
func (f *decompressor) huffmanBlock(hl, hd *huffmanDecoder) {
readLiteral:
	// Read literal and/or (length, distance) according to RFC section 3.2.3.
	{
		v := f.huffSym(hl)
		var n uint // number of bits extra
		var length int
		switch {
		case v < 256:
			f.rp.big = append(f.rp.big, byte(v))
			goto readLiteral
		case v == 256:
			return // end of block
		// otherwise, reference to older data
		case v < 265:
			length = v - (257 - 3)
			n = 0
		case v < 269:
			length = v*2 - (265*2 - 11)
			n = 1
		case v < 273:
			length = v*4 - (269*4 - 19)
			n = 2
		case v < 277:
			length = v*8 - (273*8 - 35)
			n = 3
		case v < 281:
			length = v*16 - (277*16 - 67)
			n = 4
		case v < 285:
			length = v*32 - (281*32 - 131)
			n = 5
		case v < maxNumLit:
			length = 258
			n = 0
		default:
			panic("corrupt DEFLATE")
		}
		if n > 0 {
			for f.rp.nb < n {
				f.moreBits()
			}
			length += int(f.rp.b & uint32(1<<n-1))
			f.rp.b >>= n
			f.rp.nb -= n
		}

		var dist int
		if hd == nil {
			for f.rp.nb < 5 {
				f.moreBits()
			}
			dist = int(bits.Reverse8(uint8(f.rp.b & 0x1F << 3)))
			f.rp.b >>= 5
			f.rp.nb -= 5
		} else {
			dist = f.huffSym(hd)
		}

		switch {
		case dist < 4:
			dist++
		case dist < maxNumDist:
			nb := uint(dist-2) >> 1
			// have 1 bit in bottom of dist, need nb more.
			extra := (dist & 1) << nb
			for f.rp.nb < nb {
				f.moreBits()
			}
			extra |= int(f.rp.b & uint32(1<<nb-1))
			f.rp.b >>= nb
			f.rp.nb -= nb
			dist = 1<<(nb+1) + 1 + extra
		default:
			panic("corrupt DEFLATE")
		}

		// No check on length; encoding can be prescient.
		if dist > maxMatchOffset {
			panic("corrupt DEFLATE")
		}

		for range length {
			f.rp.big = append(f.rp.big, f.rp.big[len(f.rp.big)-dist])
		}
		goto readLiteral
	}
}

// Copy a single uncompressed data block from input to output.
func (f *decompressor) dataBlock() {
	// Uncompressed.
	// Discard current half-byte.
	f.rp.nb = 0
	f.rp.b = 0

	// Length then ones-complement of length.
	var buf [4]byte
	nr, err := io.ReadFull(f.r, buf[0:4])
	f.rp.roffset += int64(nr)
	if err != nil {
		panic("corrupt DEFLATE")
	}
	n := int(buf[0]) | int(buf[1])<<8
	nn := int(buf[2]) | int(buf[3])<<8
	if uint16(nn) != uint16(^n) {
		panic("corrupt DEFLATE")
	}

	for range n {
		b, err := f.r.ReadByte()
		if err != nil {
			panic("corrupt DEFLATE")
		}
		f.rp.roffset++
		f.rp.big = append(f.rp.big, b)
	}
}

func (f *decompressor) moreBits() {
	c, err := f.r.ReadByte()
	if err != nil {
		panic("corrupt DEFLATE")
	}
	f.rp.roffset++
	f.rp.b |= uint32(c) << f.rp.nb
	f.rp.nb += 8
}

// Read the next Huffman-encoded symbol from f according to h.
func (f *decompressor) huffSym(h *huffmanDecoder) int {
	// Since a huffmanDecoder can be empty or be composed of a degenerate tree
	// with single element, huffSym must error on these two edge cases. In both
	// cases, the chunks slice will be 0 for the invalid sequence, leading it
	// satisfy the n == 0 check below.
	n := uint(h.min)
	// Optimization. Compiler isn't smart enough to keep f.rp.b,f.rp.nb in registers,
	// but is smart enough to keep local variables in registers, so use nb and b,
	// inline call to moreBits and reassign b,nb back to f on return.
	nb, b := f.rp.nb, f.rp.b
	for {
		for nb < n {
			c, err := f.r.ReadByte()
			if err != nil {
				f.rp.b = b
				f.rp.nb = nb
				panic("corrupt DEFLATE")
			}
			f.rp.roffset++
			b |= uint32(c) << (nb & 31)
			nb += 8
		}
		chunk := h.chunks[b&(huffmanNumChunks-1)]
		n = uint(chunk & huffmanCountMask)
		if n > huffmanChunkBits {
			chunk = h.links[chunk>>huffmanValueShift][(b>>huffmanChunkBits)&h.linkMask]
			n = uint(chunk & huffmanCountMask)
		}
		if n <= nb {
			if n == 0 {
				f.rp.b = b
				f.rp.nb = nb
				panic("corrupt DEFLATE")
			}
			f.rp.b = b >> (n & 31)
			f.rp.nb = nb - n
			return int(chunk >> huffmanValueShift)
		}
	}
}

func fixedHuffmanDecoderInit() {
	fixedOnce.Do(func() {
		// These come from the RFC section 3.2.6.
		var bits [288]int
		for i := 0; i < 144; i++ {
			bits[i] = 8
		}
		for i := 144; i < 256; i++ {
			bits[i] = 9
		}
		for i := 256; i < 280; i++ {
			bits[i] = 7
		}
		for i := 280; i < 288; i++ {
			bits[i] = 8
		}
		fixedHuffmanDecoder.init(bits[:])
	})
}
