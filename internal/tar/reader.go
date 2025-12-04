// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tar

import (
	"bytes"
	"io"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
)

func New(r io.ReaderAt) fs.FS {
	return New2(r, r)
}

// New2 routes headers and data requests through different readers, to help exotic caching schemes
func New2(headerReader, dataReader io.ReaderAt) fs.FS {
	fsys := fskeleton.New()
	go populate(fsys, headerReader, dataReader) // yes, discard the error
	return fsys
}

func populate(fsys *fskeleton.FS, headerReader, dataReader io.ReaderAt) error {
	defer fsys.NoMore()
	var paxHdrs map[string]string
	var gnuLongName, gnuLongLink string
	var rawHdr block
	off := int64(0)

	for {
		n, err := headerReader.ReadAt(rawHdr[:], off)
		if n < len(rawHdr) {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		off += int64(len(rawHdr))

		hdr, err := readHeader(&rawHdr)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		size := hdr.Size
		if isHeaderOnlyType(hdr.Typeflag) {
			size = 0
		}
		nextHeader := (off + size + blockSize - 1) & -blockSize

		// Check for PAX/GNU special headers and files.
		switch hdr.Typeflag {
		case TypeXGlobalHeader: // ignore
		case TypeXHeader:
			paxHdrs, err = parsePAX(io.NewSectionReader(headerReader, off, size))
			if err != nil {
				return err
			}
			// This is a meta header affecting the next header
		case TypeGNULongName, TypeGNULongLink:
			realname, err := readSpecialFile(io.NewSectionReader(headerReader, off, size))
			if err != nil {
				return err
			}
			var p parser
			switch hdr.Typeflag {
			case TypeGNULongName:
				gnuLongName = p.parseString(realname)
			case TypeGNULongLink:
				gnuLongLink = p.parseString(realname)
			}
			// This is a meta header affecting the next header
		default:
			// The old GNU sparse format is handled here since it is technically
			// just a regular file with additional attributes.

			if err := mergePAX(hdr, paxHdrs); err != nil {
				return err
			}
			if gnuLongName != "" {
				hdr.Name = gnuLongName
			}
			if gnuLongLink != "" {
				hdr.Linkname = gnuLongLink
			}
			if hdr.Typeflag == TypeRegA {
				if strings.HasSuffix(hdr.Name, "/") {
					hdr.Typeflag = TypeDir // Legacy archives use trailing slash for directories
				} else {
					hdr.Typeflag = TypeReg
				}
			}

			// One of the sparse-file formats can read a few more 512-byte header blocks
			moreHdr := io.NewSectionReader(headerReader, off, math.MaxInt64)
			sph, err := getSparseHoles(hdr, &rawHdr, moreHdr)
			if err != nil {
				return err
			}
			extendedHeader, _ := moreHdr.Seek(0, io.SeekCurrent)

			// The extended headers advance the actual data offset.
			// We can infer the physical size from the updated total logical size, and the holes list.
			off += extendedHeader
			nextHeader = off
			if !isHeaderOnlyType(hdr.Typeflag) {
				nextHeader += hdr.Size
				for _, hole := range sph {
					nextHeader -= hole.Length
				}
				nextHeader = (nextHeader + blockSize - 1) & -blockSize
			}

			cleanPath := strings.TrimLeft(path.Clean(hdr.Name), "/")
			if cleanPath == "" {
				cleanPath = "."
			}

			// The two main differences from archive/tar: random access and io/fs support
			reader, logisize := readerFromSparseHoles(dataReader, off, hdr.Size, sph)
			switch hdr.Typeflag {
			case TypeReg, TypeGNUSparse:
				fsys.CreateReaderAtFile(cleanPath, off, reader, logisize, fs.FileMode(hdr.Mode), hdr.ModTime, hdr)
			case TypeDir:
				fsys.CreateDir(cleanPath, fs.FileMode(hdr.Mode), hdr.ModTime, hdr)
			case TypeSymlink:
				targ := path.Join(cleanPath, "..", hdr.Linkname)
				if targ == ".." || strings.HasPrefix(targ, "../") {
					targ = ""
				}
				fsys.CreateSymlink(cleanPath, targ, fs.FileMode(hdr.Mode), hdr.ModTime, hdr)
			}

			gnuLongLink, gnuLongName, paxHdrs = "", "", nil
		}
		off = nextHeader
	}
	return nil
}

// Split according to the io/fs rules:
func splitPath(name string) (dir, base string) {
	slash := strings.LastIndexByte(name, '/')
	if slash == -1 {
		return ".", name
	} else {
		return name[:slash], name[slash+1:]
	}
}

func getSparseHoles(hdr *Header, rawHdr *block, moreHdr io.Reader) (sparseHoles, error) {
	var spd []sparseEntry
	var err error
	if hdr.Typeflag == TypeGNUSparse {
		spd, err = readOldGNUSparseMap(hdr, rawHdr, moreHdr)
	} else {
		spd, err = readGNUSparsePAXHeaders(hdr, moreHdr)
	}

	// If sp is non-nil, then this is a sparse file.
	// Note that it is possible for len(sp) == 0.
	if err == nil && spd != nil {
		if isHeaderOnlyType(hdr.Typeflag) || !validateSparseEntries(spd, hdr.Size) {
			return nil, ErrHeader
		}
		spd = invertSparseEntries(spd, hdr.Size) // convert to "holes"
	}
	return spd, err
}

// readGNUSparsePAXHeaders checks the PAX headers for GNU sparse headers.
// If they are found, then this function reads the sparse map and returns it.
// This assumes that 0.0 headers have already been converted to 0.1 headers
// by the PAX header parsing logic.
func readGNUSparsePAXHeaders(hdr *Header, more io.Reader) (sparseDatas, error) {
	// Identify the version of GNU headers.
	var is1x0 bool
	major, minor := hdr.PAXRecords[paxGNUSparseMajor], hdr.PAXRecords[paxGNUSparseMinor]
	switch {
	case major == "0" && (minor == "0" || minor == "1"):
		is1x0 = false
	case major == "1" && minor == "0":
		is1x0 = true
	case major != "" || minor != "":
		return nil, nil // Unknown GNU sparse PAX version
	case hdr.PAXRecords[paxGNUSparseMap] != "":
		is1x0 = false // 0.0 and 0.1 did not have explicit version records, so guess
	default:
		return nil, nil // Not a PAX format GNU sparse file.
	}

	// Update hdr from GNU sparse PAX headers.
	if name := hdr.PAXRecords[paxGNUSparseName]; name != "" {
		hdr.Name = name
	}
	size := hdr.PAXRecords[paxGNUSparseSize]
	if size == "" {
		size = hdr.PAXRecords[paxGNUSparseRealSize]
	}
	if size != "" {
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil {
			return nil, ErrHeader
		}
		hdr.Size = n
	}

	// Read the sparse map according to the appropriate format.
	if is1x0 {
		return readGNUSparseMap1x0(more)
	}
	return readGNUSparseMap0x1(hdr.PAXRecords)
}

// mergePAX merges paxHdrs into hdr for all relevant fields of Header.
func mergePAX(hdr *Header, paxHdrs map[string]string) (err error) {
	for k, v := range paxHdrs {
		if v == "" {
			continue // Keep the original USTAR value
		}
		var id64 int64
		switch k {
		case paxPath:
			hdr.Name = v
		case paxLinkpath:
			hdr.Linkname = v
		case paxUname:
			hdr.Uname = v
		case paxGname:
			hdr.Gname = v
		case paxUid:
			id64, err = strconv.ParseInt(v, 10, 64)
			hdr.Uid = int(id64) // Integer overflow possible
		case paxGid:
			id64, err = strconv.ParseInt(v, 10, 64)
			hdr.Gid = int(id64) // Integer overflow possible
		case paxAtime:
			hdr.AccessTime, err = parsePAXTime(v)
		case paxMtime:
			hdr.ModTime, err = parsePAXTime(v)
		case paxCtime:
			hdr.ChangeTime, err = parsePAXTime(v)
		case paxSize:
			hdr.Size, err = strconv.ParseInt(v, 10, 64)
		default:
			if strings.HasPrefix(k, paxSchilyXattr) {
				if hdr.Xattrs == nil {
					hdr.Xattrs = make(map[string]string)
				}
				hdr.Xattrs[k[len(paxSchilyXattr):]] = v
			}
		}
		if err != nil {
			return ErrHeader
		}
	}
	hdr.PAXRecords = paxHdrs
	return nil
}

// parsePAX parses PAX headers.
// If an extended header (type 'x') is invalid, ErrHeader is returned.
func parsePAX(r io.Reader) (map[string]string, error) {
	buf, err := readSpecialFile(r)
	if err != nil {
		return nil, err
	}
	sbuf := string(buf)

	// For GNU PAX sparse format 0.0 support.
	// This function transforms the sparse format 0.0 headers into format 0.1
	// headers since 0.0 headers were not PAX compliant.
	var sparseMap []string

	paxHdrs := make(map[string]string)
	for len(sbuf) > 0 {
		key, value, residual, err := parsePAXRecord(sbuf)
		if err != nil {
			return nil, ErrHeader
		}
		sbuf = residual

		switch key {
		case paxGNUSparseOffset, paxGNUSparseNumBytes:
			// Validate sparse header order and value.
			if (len(sparseMap)%2 == 0 && key != paxGNUSparseOffset) ||
				(len(sparseMap)%2 == 1 && key != paxGNUSparseNumBytes) ||
				strings.Contains(value, ",") {
				return nil, ErrHeader
			}
			sparseMap = append(sparseMap, value)
		default:
			paxHdrs[key] = value
		}
	}
	if len(sparseMap) > 0 {
		paxHdrs[paxGNUSparseMap] = strings.Join(sparseMap, ",")
	}
	return paxHdrs, nil
}

// readHeader reads the next block header.
// It returns the raw block of the
// header in case further processing is required.
//
// The err will be set to io.EOF if the block is zero or the file ends
func readHeader(blk *block) (*Header, error) {
	if bytes.Equal(blk[:], zeroBlock[:]) {
		return nil, io.EOF
	}

	// Verify the header matches a known format.
	format := blk.getFormat()
	if format == FormatUnknown {
		return nil, ErrHeader
	}

	var p parser
	hdr := new(Header)

	// Unpack the V7 header.
	v7 := blk.toV7()
	hdr.Typeflag = v7.typeFlag()[0]
	hdr.Name = p.parseString(v7.name())
	hdr.Linkname = p.parseString(v7.linkName())
	hdr.Size = p.parseNumeric(v7.size())
	hdr.Mode = p.parseNumeric(v7.mode())
	hdr.Uid = int(p.parseNumeric(v7.uid()))
	hdr.Gid = int(p.parseNumeric(v7.gid()))
	hdr.ModTime = time.Unix(p.parseNumeric(v7.modTime()), 0)

	// Unpack format specific fields.
	if format > formatV7 {
		ustar := blk.toUSTAR()
		hdr.Uname = p.parseString(ustar.userName())
		hdr.Gname = p.parseString(ustar.groupName())
		hdr.Devmajor = p.parseNumeric(ustar.devMajor())
		hdr.Devminor = p.parseNumeric(ustar.devMinor())

		var prefix string
		switch {
		case format.has(FormatUSTAR | FormatPAX):
			hdr.Format = format
			ustar := blk.toUSTAR()
			prefix = p.parseString(ustar.prefix())

			// For Format detection, check if block is properly formatted since
			// the parser is more liberal than what USTAR actually permits.
			notASCII := func(r rune) bool { return r >= 0x80 }
			if bytes.IndexFunc(blk[:], notASCII) >= 0 {
				hdr.Format = FormatUnknown // Non-ASCII characters in block.
			}
			nul := func(b []byte) bool { return int(b[len(b)-1]) == 0 }
			if !(nul(v7.size()) && nul(v7.mode()) && nul(v7.uid()) && nul(v7.gid()) &&
				nul(v7.modTime()) && nul(ustar.devMajor()) && nul(ustar.devMinor())) {
				hdr.Format = FormatUnknown // Numeric fields must end in NUL
			}
		case format.has(formatSTAR):
			star := blk.toSTAR()
			prefix = p.parseString(star.prefix())
			hdr.AccessTime = time.Unix(p.parseNumeric(star.accessTime()), 0)
			hdr.ChangeTime = time.Unix(p.parseNumeric(star.changeTime()), 0)
		case format.has(FormatGNU):
			hdr.Format = format
			var p2 parser
			gnu := blk.toGNU()
			if b := gnu.accessTime(); b[0] != 0 {
				hdr.AccessTime = time.Unix(p2.parseNumeric(b), 0)
			}
			if b := gnu.changeTime(); b[0] != 0 {
				hdr.ChangeTime = time.Unix(p2.parseNumeric(b), 0)
			}

			// Prior to Go1.8, the Writer had a bug where it would output
			// an invalid tar file in certain rare situations because the logic
			// incorrectly believed that the old GNU format had a prefix field.
			// This is wrong and leads to an output file that mangles the
			// atime and ctime fields, which are often left unused.
			//
			// In order to continue reading tar files created by former, buggy
			// versions of Go, we skeptically parse the atime and ctime fields.
			// If we are unable to parse them and the prefix field looks like
			// an ASCII string, then we fallback on the pre-Go1.8 behavior
			// of treating these fields as the USTAR prefix field.
			//
			// Note that this will not use the fallback logic for all possible
			// files generated by a pre-Go1.8 toolchain. If the generated file
			// happened to have a prefix field that parses as valid
			// atime and ctime fields (e.g., when they are valid octal strings),
			// then it is impossible to distinguish between a valid GNU file
			// and an invalid pre-Go1.8 file.
			//
			// See https://golang.org/issues/12594
			// See https://golang.org/issues/21005
			if p2.err != nil {
				hdr.AccessTime, hdr.ChangeTime = time.Time{}, time.Time{}
				ustar := blk.toUSTAR()
				if s := p.parseString(ustar.prefix()); isASCII(s) {
					prefix = s
				}
				hdr.Format = FormatUnknown // Buggy file is not GNU
			}
		}
		if len(prefix) > 0 {
			hdr.Name = prefix + "/" + hdr.Name
		}
	}
	return hdr, p.err
}

// readOldGNUSparseMap reads the sparse map from the old GNU sparse format.
// The sparse map is stored in the tar header if it's small enough.
// If it's larger than four entries, then one or more extension headers are used
// to store the rest of the sparse map.
//
// The Header.Size does not reflect the size of any extended headers used.
// Thus, this function will read from the raw io.Reader to fetch extra headers.
// This method mutates blk in the process.
func readOldGNUSparseMap(hdr *Header, blk *block, more io.Reader) (sparseDatas, error) {
	// Make sure that the input format is GNU.
	// Unfortunately, the STAR format also has a sparse header format that uses
	// the same type flag but has a completely different layout.
	if blk.getFormat() != FormatGNU {
		return nil, ErrHeader
	}

	var p parser
	hdr.Size = p.parseNumeric(blk.toGNU().realSize())
	if p.err != nil {
		return nil, p.err
	}
	s := blk.toGNU().sparse()
	spd := make(sparseDatas, 0, s.maxEntries())
	for {
		for i := 0; i < s.maxEntries(); i++ {
			// This termination condition is identical to GNU and BSD tar.
			if s.entry(i).offset()[0] == 0x00 {
				break // Don't return, need to process extended headers (even if empty)
			}
			offset := p.parseNumeric(s.entry(i).offset())
			length := p.parseNumeric(s.entry(i).length())
			if p.err != nil {
				return nil, p.err
			}
			spd = append(spd, sparseEntry{Offset: offset, Length: length})
		}

		if s.isExtended()[0] > 0 {
			// There are more entries. Read an extension header and parse its entries.
			if _, err := io.ReadFull(more, blk[:]); err != nil {
				return nil, err
			}
			s = blk.toSparse()
			continue
		}
		return spd, nil // Done
	}
}

// readGNUSparseMap1x0 reads the sparse map as stored in GNU's PAX sparse format
// version 1.0. The format of the sparse map consists of a series of
// newline-terminated numeric fields. The first field is the number of entries
// and is always present. Following this are the entries, consisting of two
// fields (offset, length). This function must stop reading at the end
// boundary of the block containing the last newline.
//
// Note that the GNU manual says that numeric values should be encoded in octal
// format. However, the GNU tar utility itself outputs these values in decimal.
// As such, this library treats values as being encoded in decimal.
func readGNUSparseMap1x0(r io.Reader) (sparseDatas, error) {
	var (
		cntNewline int64
		buf        bytes.Buffer
		blk        block
	)

	// feedTokens copies data in blocks from r into buf until there are
	// at least cnt newlines in buf. It will not read more blocks than needed.
	feedTokens := func(n int64) error {
		for cntNewline < n {
			if _, err := io.ReadFull(r, blk[:]); err != nil {
				return err
			}
			buf.Write(blk[:])
			for _, c := range blk {
				if c == '\n' {
					cntNewline++
				}
			}
		}
		return nil
	}

	// nextToken gets the next token delimited by a newline. This assumes that
	// at least one newline exists in the buffer.
	nextToken := func() string {
		cntNewline--
		tok, _ := buf.ReadString('\n')
		return strings.TrimRight(tok, "\n")
	}

	// Parse for the number of entries.
	// Use integer overflow resistant math to check this.
	if err := feedTokens(1); err != nil {
		return nil, err
	}
	numEntries, err := strconv.ParseInt(nextToken(), 10, 0) // Intentionally parse as native int
	if err != nil || numEntries < 0 || int(2*numEntries) < int(numEntries) {
		return nil, ErrHeader
	}

	// Parse for all member entries.
	// numEntries is trusted after this since a potential attacker must have
	// committed resources proportional to what this library used.
	if err := feedTokens(2 * numEntries); err != nil {
		return nil, err
	}
	spd := make(sparseDatas, 0, numEntries)
	for i := int64(0); i < numEntries; i++ {
		offset, err1 := strconv.ParseInt(nextToken(), 10, 64)
		length, err2 := strconv.ParseInt(nextToken(), 10, 64)
		if err1 != nil || err2 != nil {
			return nil, ErrHeader
		}
		spd = append(spd, sparseEntry{Offset: offset, Length: length})
	}
	return spd, nil
}

// readGNUSparseMap0x1 reads the sparse map as stored in GNU's PAX sparse format
// version 0.1. The sparse map is stored in the PAX headers.
func readGNUSparseMap0x1(paxHdrs map[string]string) (sparseDatas, error) {
	// Get number of entries.
	// Use integer overflow resistant math to check this.
	numEntriesStr := paxHdrs[paxGNUSparseNumBlocks]
	numEntries, err := strconv.ParseInt(numEntriesStr, 10, 0) // Intentionally parse as native int
	if err != nil || numEntries < 0 || int(2*numEntries) < int(numEntries) {
		return nil, ErrHeader
	}

	// There should be two numbers in sparseMap for each entry.
	sparseMap := strings.Split(paxHdrs[paxGNUSparseMap], ",")
	if len(sparseMap) == 1 && sparseMap[0] == "" {
		sparseMap = sparseMap[:0]
	}
	if int64(len(sparseMap)) != 2*numEntries {
		return nil, ErrHeader
	}

	// Loop through the entries in the sparse map.
	// numEntries is trusted now.
	spd := make(sparseDatas, 0, numEntries)
	for len(sparseMap) >= 2 {
		offset, err1 := strconv.ParseInt(sparseMap[0], 10, 64)
		length, err2 := strconv.ParseInt(sparseMap[1], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, ErrHeader
		}
		spd = append(spd, sparseEntry{Offset: offset, Length: length})
		sparseMap = sparseMap[2:]
	}
	return spd, nil
}

// readSpecialFile is like io.ReadAll except it returns
// ErrFieldTooLong if more than maxSpecialFileSize is read.
func readSpecialFile(r io.Reader) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, maxSpecialFileSize+1))
	if len(buf) > maxSpecialFileSize {
		return nil, ErrFieldTooLong
	}
	return buf, err
}

// discard skips n bytes in r, reporting an error if unable to do so.
func discard(r io.Reader, n int64) error {
	// If possible, Seek to the last byte before the end of the data section.
	// Do this because Seek is often lazy about reporting errors; this will mask
	// the fact that the stream may be truncated. We can rely on the
	// io.CopyN done shortly afterwards to trigger any IO errors.
	var seekSkipped int64 // Number of bytes skipped via Seek
	if sr, ok := r.(io.Seeker); ok && n > 1 {
		// Not all io.Seeker can actually Seek. For example, os.Stdin implements
		// io.Seeker, but calling Seek always returns an error and performs
		// no action. Thus, we try an innocent seek to the current position
		// to see if Seek is really supported.
		pos1, err := sr.Seek(0, io.SeekCurrent)
		if pos1 >= 0 && err == nil {
			// Seek seems supported, so perform the real Seek.
			pos2, err := sr.Seek(n-1, io.SeekCurrent)
			if pos2 < 0 || err != nil {
				return err
			}
			seekSkipped = pos2 - pos1
		}
	}

	copySkipped, err := io.CopyN(io.Discard, r, n-seekSkipped)
	if err == io.EOF && seekSkipped+copySkipped < n {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// splitUSTARPath splits a path according to USTAR prefix and suffix rules.
// If the path is not splittable, then it will return ("", "", false).
func splitUSTARPath(name string) (prefix, suffix string, ok bool) {
	length := len(name)
	if length <= nameSize || !isASCII(name) {
		return "", "", false
	} else if length > prefixSize+1 {
		length = prefixSize + 1
	} else if name[length-1] == '/' {
		length--
	}

	i := strings.LastIndex(name[:length], "/")
	nlen := len(name) - i - 1 // nlen is length of suffix
	plen := i                 // plen is length of prefix
	if i <= 0 || nlen > nameSize || nlen == 0 || plen > prefixSize {
		return "", "", false
	}
	return name[:i], name[i+1:], true
}
