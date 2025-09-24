// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Implement fs.FS for Apple's very old Hierarchical File System
// (plain HFS from 1985, not to be confused with HFS+)
package hfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/multireaderat"
)

type FS struct {
	fskeleton.FS
}

// Create a new FS from an HFS volume
func New(disk io.ReaderAt) (retfs FS, reterr error) {
	defer func() {
		if r := recover(); r != nil {
			retfs, reterr = FS{}, fmt.Errorf("%v", r)
		}
	}()

	var mdb [512]byte
	_, err := disk.ReadAt(mdb[:], 0x400)
	if err != nil {
		return FS{}, fmt.Errorf("HFS Master Directory Block unreadable: %w", err)
	}

	if mdb[0] != 'B' || mdb[1] != 'D' {
		return FS{}, errors.New("HFS magic number absent")
	}

	drNmAlBlks := binary.BigEndian.Uint16(mdb[0x12:])
	drAlBlkSiz := binary.BigEndian.Uint32(mdb[0x14:])
	drAlBlSt := binary.BigEndian.Uint16(mdb[0x1c:])

	// There is a compression format that deceptively leaves the magic number intact.
	// Attempt to detect this early by checking the image size.
	// Don't resort to an actual read, because the seek might be expensive.
	minSize := int64(drAlBlSt)*512 + int64(drAlBlkSiz)*int64(drNmAlBlks)
	if actualSize, ok := tryGetSizeCheaply(disk); ok {
		if actualSize < minSize {
			return FS{}, fmt.Errorf("likely Disk Copy compressed HFS image: expected %db but got %db", minSize, actualSize)
		}
	}

	// Open question: can the extents overflow file depend on extents stored in itself?
	overflow :=
		parseExtentsOverflow(
			parseBTree(
				mustReadAll(
					parseExtents(mdb[0x86:]).
						toBytes(drAlBlkSiz, drAlBlSt).
						makeReader(disk))))

	catalog :=
		parseBTree(
			mustReadAll(
				parseExtents(
					mdb[0x96:]).
					chaseOverflow(overflow, 4, false).
					toBytes(drAlBlkSiz, drAlBlSt).
					makeReader(disk)))

	dirs := dirPaths(catalog)

	fsys := fskeleton.New()
	for _, rec := range catalog {
		cut := (int(rec[0]) + 2) &^ 1
		val := rec[cut:]
		parent := binary.BigEndian.Uint32(rec[2:])
		switch val[0] {
		case 1: // dir
			cnid := binary.BigEndian.Uint32(val[6:])
			fsys.CreateDir(dirs[cnid], fs.FileMode(0), macTime(val[0xe:]), cnid)

			appleDoubleData := appledouble.MakePrefix(0, map[int][]byte{
				appledouble.MACINTOSH_FILE_INFO: append(val[2:4:4], make([]byte, 2)...),
				appledouble.FINDER_INFO:         val[0x16:0x36],
				appledouble.FILE_DATES_INFO:     append(val[0xa:0x16:0x16], make([]byte, 4)...), // cr/md/bk/acc
			})
			readerAt := bytes.NewReader(appleDoubleData)
			fsys.CreateRandomAccessFile(dirs[cnid], readerAt, readerAt.Size(), 0, macTime(val[0xe:]), cnid)

		case 2: // file
			cnid := binary.BigEndian.Uint32(val[0x14:])
			name := path.Join(dirs[parent], strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":"))
			mtime := macTime(val[0x30:])

			dfSize, rfSize := int64(binary.BigEndian.Uint32(val[0x1a:])), int64(binary.BigEndian.Uint32(val[0x24:]))
			df := parseExtents(val[0x4a:]).
				chaseOverflow(overflow, cnid, false).
				toBytes(drAlBlkSiz, drAlBlSt).
				clipExtents(dfSize).
				makeReader(disk)
			rf := parseExtents(val[0x56:]).
				chaseOverflow(overflow, cnid, true).
				toBytes(drAlBlkSiz, drAlBlSt).
				clipExtents(int64(rfSize)).
				makeReader(disk)

			adHdr := appledouble.MakePrefix(uint32(rfSize),
				map[int][]byte{
					appledouble.MACINTOSH_FILE_INFO: append(val[2:4:4], make([]byte, 2)...),
					appledouble.FINDER_INFO:         append(val[0x4:0x14:0x14], val[0x38:0x48]...),
					appledouble.FILE_DATES_INFO:     append(val[0x2c:0x38:0x38], make([]byte, 4)...), // cr/md/bk/acc
				})
			adReader := multireaderat.New(bytes.NewReader(adHdr), rf)

			fsys.CreateRandomAccessFile(name, df, dfSize, 0, mtime, cnid)
			fsys.CreateRandomAccessFile(appleDoubleName(name), adReader, adReader.Size(), 0, mtime, cnid)
		}

	}
	fsys.NoMore()
	return FS{fsys}, nil
}

// For chasing through the extents overflow file
type extKey struct {
	cnid  uint32
	n     uint16
	isres bool
}

// So these types can be used as a method receiver, enabling.method.chains
type blockExtents []uint16 // alternating (firstblock, blockcount)
type byteExtents []int64   // alternating (firstbyte, bytecount)

func (x byteExtents) makeReader(fs io.ReaderAt) multireaderat.SizeReaderAt {
	subreaders := make([]multireaderat.SizeReaderAt, 0, len(x)/2)
	for i := 0; i < len(x); i += 2 {
		xstart, xlen := x[i], x[i+1]
		subreaders = append(subreaders, io.NewSectionReader(fs, xstart, xlen))
	}
	return multireaderat.New(subreaders...)
}

func mustReadAll(r multireaderat.SizeReaderAt) []byte {
	b := make([]byte, r.Size())
	_, err := r.ReadAt(b, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		panic("unable to read a special file")
	}
	return b
}

func parseExtents(record []byte) blockExtents {
	var extents blockExtents
	for i := 0; i < 12; i += 4 {
		start := binary.BigEndian.Uint16(record[i:])
		count := binary.BigEndian.Uint16(record[i+2:])
		if count != 0 {
			extents = append(extents, start, count)
		}
	}
	return extents
}

func (x blockExtents) chaseOverflow(overflow map[extKey]blockExtents, cnid uint32, isres bool) blockExtents {
	nblocks := uint16(0)
	for i := 1; i < len(x); i += 2 {
		nblocks += x[i]
	}

	for {
		if moreExtents, ok := overflow[extKey{cnid, nblocks, isres}]; ok {
			x = append(x, moreExtents...)

			for i := 1; i < len(moreExtents); i += 2 {
				nblocks += moreExtents[i]
			}
		} else {
			break
		}
	}

	return x
}

func (a blockExtents) toBytes(drAlBlkSiz uint32, drAlBlSt uint16) byteExtents {
	b := make(byteExtents, 0, len(a))
	for i := 0; i < len(a); i += 2 {
		start := int64(a[i])*int64(drAlBlkSiz) + int64(drAlBlSt)*512
		len := int64(a[i+1]) * int64(drAlBlkSiz)
		b = append(b, start, len)
	}
	return b
}

func (a byteExtents) clipExtents(size int64) byteExtents {
	sofar := int64(0)
	for i := 0; i < len(a); i += 2 {
		xsize := a[i+1]

		if xsize > size-sofar {
			xsize = size - sofar
		}

		a[i+1] = xsize

		sofar += xsize

		if xsize == 0 {
			a = a[:i]
			break
		} else if sofar == size {
			a = a[:i+2]
			break
		}
	}
	if sofar != size {
		panic("not enough extents to satisfy logical length")
	}
	return a
}

func parseBTree(tree []byte) (records [][]byte) {
	// Special first node has special header record
	headerRec := parseBNode(tree)[0]

	// Ends of a linked list of leaf nodes
	bthFNode := int(binary.BigEndian.Uint32(headerRec[10:]))
	bthLNode := int(binary.BigEndian.Uint32(headerRec[14:]))

	i := bthFNode
	for {
		offset := 512 * i
		records = append(records, parseBNode(tree[offset:][:512])...)

		if i == bthLNode {
			break
		}
		i = int(binary.BigEndian.Uint32(tree[offset:]))
	}

	return records
}

func parseBNode(node []byte) [][]byte {
	cnt := int(binary.BigEndian.Uint16(node[10:]))

	boundaries := make([]int, 0, cnt+1)
	for i := 0; i < cnt+1; i++ {
		boundaries = append(boundaries, int(binary.BigEndian.Uint16(node[512-2-2*i:])))
	}

	records := make([][]byte, 0, cnt)
	for i := 0; i < cnt; i++ {
		start := boundaries[i]
		stop := boundaries[i+1]
		records = append(records, node[start:stop])
	}

	return records
}

func parseExtentsOverflow(btree [][]byte) map[extKey]blockExtents {
	ret := make(map[extKey]blockExtents)

	for _, record := range btree {
		// xkrKeyLen always 7 in the "current implementation"
		if record[0] != 7 {
			continue
		}

		ret[extKey{
			cnid:  binary.BigEndian.Uint32(record[2:]),
			n:     binary.BigEndian.Uint16(record[6:]),
			isres: record[1] == 0xff,
		}] = parseExtents(record[8:])
	}

	return ret
}

// For reproducibility, pretends that all times on the volume mean UTC,
// even though they were unfortunately set to local time and the TZ discarded
func macTime(field []byte) time.Time {
	stamp := binary.BigEndian.Uint32(field)
	return time.Unix(int64(stamp)-2082844800, 0).UTC()
}

func tryGetSizeCheaply(f io.ReaderAt) (int64, bool) {
	switch as := f.(type) {
	case interface{ Size() int64 }:
		return as.Size(), true
	case fs.File:
		stat, err := as.Stat()
		if err != nil {
			return 0, false
		}
		return stat.Size(), true
	default:
		return 0, false
	}
}

func dirPaths(catalog [][]byte) map[uint32]string {
	tree := make(map[uint32][]dir)
	for _, rec := range catalog {
		cut := (int(rec[0]) + 2) &^ 1
		val := rec[cut:]
		parent := binary.BigEndian.Uint32(rec[2:])
		if val[0] != 1 { // directories only
			continue
		}
		cnid := binary.BigEndian.Uint32(val[6:])
		name := strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":")
		tree[parent] = append(tree[parent], dir{name, cnid})
	}
	m := make(map[uint32]string)
	dirRecurse(m, tree, 1, ".")
	return m
}

func dirRecurse(m map[uint32]string, tree map[uint32][]dir, cnid uint32, name string) {
	for _, ch := range tree[cnid] {
		sub := path.Join(name, ch.name)
		m[ch.cnid] = sub
		dirRecurse(m, tree, ch.cnid, sub)
	}
}

type dir struct {
	name string
	cnid uint32
}

func appleDoubleName(name string) string {
	d, b := path.Split(name)
	return d + "._" + b
}
