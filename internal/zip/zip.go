// Copyright Elliot Nunn. Portions copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package zip is an alternative implementation suited to BeHierarchic
// - understands AppleDouble files: __MACOSX/**/._*
// - touches the headers as little as possible
// - exposes uncompressed files as [io.ReaderAt]
// - uses fskeleton to intern paths and save RAM
package zip

import (
	"cmp"
	"compress/bzip2"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
)

var (
	ErrFormat    = errors.New("zip: not a valid zip file")
	ErrAlgorithm = errors.New("zip: unsupported compression algorithm")
	ErrChecksum  = errors.New("zip: checksum error")
	ErrNoSpanned = errors.New("zip: spanned archives not supported")
)

// New opens an Zip file
func New(r io.ReaderAt, size int64) (fs.FS, error) {
	return New2(r, r, size)
}

// New2 routes headers and data requests through different readers, to help exotic caching schemes
func New2(headerReader, dataReader io.ReaderAt, size int64) (fs.FS, error) {
	eocd, err := getEOCD(headerReader, size)
	if err != nil {
		return nil, err
	}

	eocdOffset := size - int64(len(eocd))
	thisDisk := uint32(binary.LittleEndian.Uint16(eocd[4:]))
	centralDisk := uint32(binary.LittleEndian.Uint16(eocd[6:]))
	// recordsThisDisk := uint64(binary.LittleEndian.Uint16(eocd[8:]))
	recordsTotal := uint64(binary.LittleEndian.Uint16(eocd[10:]))
	centralSize := int64(binary.LittleEndian.Uint32(eocd[12:]))
	centralOffset := int64(binary.LittleEndian.Uint32(eocd[16:]))

	sixtyFour := recordsTotal == 0xffff || centralSize == 0xffff || centralOffset == 0xffffffff
	if sixtyFour {
		locator := make([]byte, 20)
		if int64(len(locator)+len(eocd)) > size {
			return nil, ErrFormat
		}
		n, err := headerReader.ReadAt(locator, size-int64(len(eocd))-int64(len(locator)))
		if n < len(locator) {
			return nil, err
		}
		if string(locator[:4]) != "PK\x06\x07" {
			return nil, ErrFormat
		}
		eocd64Disk := binary.LittleEndian.Uint32(locator[4:])
		eocdOffset = int64(binary.LittleEndian.Uint64(locator[8:]))
		totalDisks := binary.LittleEndian.Uint32(locator[16:])
		if eocd64Disk != 0 || totalDisks != 1 {
			return nil, ErrNoSpanned
		}
		eocd64 := make([]byte, 56)
		n, err = headerReader.ReadAt(eocd64, eocdOffset)
		if n < len(eocd64) {
			return nil, err
		}
		if string(eocd64[:4]) != "PK\x06\x06" {
			return nil, ErrFormat
		}
		thisDisk = binary.LittleEndian.Uint32(eocd64[16:])
		centralDisk = binary.LittleEndian.Uint32(eocd64[20:])
		// recordsThisDisk = binary.LittleEndian.Uint64(eocd64[24:])
		recordsTotal = binary.LittleEndian.Uint64(eocd64[32:])
		centralSize = int64(binary.LittleEndian.Uint64(eocd64[40:]))
		centralOffset = int64(binary.LittleEndian.Uint64(eocd64[48:]))
	}
	// yay, now we can explore the central directory
	if thisDisk != 0 || centralDisk != 0 {
		return nil, ErrNoSpanned
	}

	// Fix zip files that are carelessly appended to non-zip data,
	// the creating program unaware of the leading data.
	// Won't work with ZIP64 files because we have to trust the EOCD64 locator.
	baseCorrection := eocdOffset - centralSize - centralOffset

	// The stdlib zip does not trust the stated central directory record size,
	// so neither do we
	if centralOffset > eocdOffset {
		return nil, ErrFormat
	}
	dir := make([]byte, eocdOffset-centralOffset)
	n, err := headerReader.ReadAt(dir, baseCorrection+centralOffset)
	if n != len(dir) {
		return nil, err
	}

	fsys := fskeleton.New()
	defer fsys.NoMore()

	type task struct {
		order  int64
		action func()
	}
	var tasks []task

	for len(dir) >= 0 {
		thisDirEntryOffset := eocdOffset - int64(len(dir))
		if len(dir) < 46 || string(dir[:4]) != "PK\x01\x02" {
			break
		}
		os := dir[5]
		// flags := binary.LittleEndian.Uint16(dir[8:])
		method := binary.LittleEndian.Uint16(dir[10:])
		dostime := binary.LittleEndian.Uint16(dir[12:])
		dosdate := binary.LittleEndian.Uint16(dir[14:])
		crc32 := binary.LittleEndian.Uint32(dir[16:])
		packed := int64(binary.LittleEndian.Uint32(dir[20:]))
		unpacked := int64(binary.LittleEndian.Uint32(dir[24:]))
		namelen := int(binary.LittleEndian.Uint16(dir[28:]))
		extralen := int(binary.LittleEndian.Uint16(dir[30:]))
		commentlen := int(binary.LittleEndian.Uint16(dir[32:]))
		attrs := binary.LittleEndian.Uint32(dir[38:])
		loc := int64(binary.LittleEndian.Uint32(dir[42:]))
		if len(dir) < 46+namelen+extralen+commentlen {
			break
		}
		dir = dir[46:]
		name := string(dir[:namelen])
		dir = dir[namelen:]
		extra := parseExtra(dir[:extralen])
		dir = dir[extralen:]
		dir = dir[commentlen:]

		if nx, ok := extra[0x7055]; ok && len(nx) >= 6 && nx[0] == 1 {
			name = string(nx[5:])
		}
		name = unicode(name)
		name = strings.TrimPrefix(name, "/")
		if strings.HasPrefix(name, "__MACOSX/") {
			if strings.HasPrefix(path.Base(name), "._") {
				name = name[9:] // AppleDouble file
			} else {
				continue // directory supporting
			}
		}
		name, isdir := strings.CutSuffix(name, "/")
		if !fs.ValidPath(name) {
			continue
		}

		mtime := msDosTimeToTime(dosdate, dostime)
		for _, k := range slices.Backward(slices.Sorted(maps.Keys(extra))) {
			t := timeFromExtraField(k, extra[k])
			if !t.IsZero() {
				mtime = t
			}
		}

		if sixtyFour {
			fields := extra[1]
			for _, shortField := range []*int64{&unpacked, &packed, &loc} {
				if *shortField == 0xffffffff && len(fields) >= 8 {
					*shortField = int64(binary.LittleEndian.Uint64(fields))
					fields = fields[8:]
				}
			}
		}

		var mode fs.FileMode
		switch os {
		case 3, 19: // Unix, Mac OS X
			mode = unixModeToFileMode(attrs >> 16)
		case 0, 11, 14: // DOS, NTFS, VFAT
			mode = msdosModeToFileMode(attrs)
		default:
			if isdir {
				mode = 0o755
			} else {
				mode = 0o644
			}

		}

		if mode&fs.ModeSymlink != 0 {
			packedReader := &localHeaderReader{r: headerReader, offset: baseCorrection + loc, size: packed}
			targbuf := make([]byte, packed)
			n, _ := packedReader.ReadAt(targbuf, 0)
			targ := ""
			if n == len(targbuf) {
				targ = unicode(string(targbuf))
				targ = path.Join(name, "..", targ)
			}
			if !fs.ValidPath(targ) {
				targ = "."
			}
			fsys.Symlink(name, baseCorrection+loc, targ, mode, mtime)
		} else if isdir {
			fsys.Mkdir(name, thisDirEntryOffset, mode, mtime)
		} else {
			tasks = append(tasks, task{loc, func() {
				fileOffset := baseCorrection + loc
				switch method {
				case 0:
					packedReader := &localHeaderReader{r: dataReader, offset: fileOffset, size: packed}
					r := newChecksumReaderAt(packedReader, unpacked, crc32)
					fsys.CreateReaderAt(name, baseCorrection+loc, r, unpacked, mode, mtime)
				case 8:
					readerFunc := func() (io.ReadCloser, error) {
						packedReader := &localHeaderReader{r: dataReader, offset: fileOffset, size: packed}
						r := flate.NewReader(io.NewSectionReader(packedReader, 0, packed))
						return newChecksumReader(r, unpacked, crc32), nil
					}
					fsys.CreateReadCloser(name, baseCorrection+loc, readerFunc, unpacked, mode, mtime)
				case 12:
					readerFunc := func() (io.Reader, error) {
						packedReader := &localHeaderReader{r: dataReader, offset: fileOffset, size: packed}
						r := bzip2.NewReader(io.NewSectionReader(packedReader, 0, packed))
						return newChecksumReader(r, unpacked, crc32), nil
					}
					fsys.CreateReader(name, baseCorrection+loc, readerFunc, unpacked, mode, mtime)
				default:
					fsys.CreateError(name, baseCorrection+loc, fmt.Errorf("%w: %d", ErrAlgorithm, method), unpacked, mode, mtime)
				}
			}})
		}
	}
	slices.SortStableFunc(tasks, func(a, b task) int { return cmp.Compare(a.order, b.order) })
	for _, task := range tasks {
		task.action()
	}
	return fsys, nil
}

type localHeaderReader struct {
	r      io.ReaderAt
	offset int64
	size   int64
	once   sync.Once
	err    error
}

func (g *localHeaderReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if off >= g.size {
		return 0, io.EOF
	}

	g.once.Do(func() {
		buf := make([]byte, 30)
		n, err := g.r.ReadAt(buf, g.offset)
		if n < len(buf) {
			g.err = err
		}
		if string(buf[:4]) != "PK\x03\x04" {
			g.err = errors.New("corrupt/absent local file header")
		}
		g.offset += 30 +
			int64(binary.LittleEndian.Uint16(buf[26:])) + // filename field
			int64(binary.LittleEndian.Uint16(buf[28:])) // extra field
	})

	if g.err != nil {
		return 0, g.err
	}

	tooLong := false
	if off+int64(len(p)) > g.size {
		p = p[:g.size-off]
		tooLong = true
	}

	n, err := g.r.ReadAt(p, g.offset+off)
	if err == nil && tooLong {
		err = io.EOF
	}
	return n, err
}

func unicode(s string) string {
	for _, rune := range s {
		if rune == 0xfffd {
			goto bad
		}
	}
	return s
bad:
	var b strings.Builder
	for _, byte := range []byte(s) {
		if byte < 128 && byte != '%' {
			b.WriteByte(byte)
		} else {
			fmt.Fprintf(&b, "%%%02x", byte)
		}
	}
	return b.String()
}

func parseExtra(x []byte) map[int][]byte {
	ret := make(map[int][]byte)
	for len(x) >= 4 {
		kind := int(binary.LittleEndian.Uint16(x))
		size := int(binary.LittleEndian.Uint16(x[2:]))
		if len(x) < 4+size {
			break
		}
		ret[kind] = x[4:][:size]
		x = x[4+size:]
	}
	return ret
}

// getEOCD reads the End of Directory Record.
//
// To avoid cache pollution, no bytes outside the EOCD are read,
// but for speed, the largest chunks possible are read (up to 22 bytes).
func getEOCD(r io.ReaderAt, size int64) ([]byte, error) {
	if size < 22 {
		return nil, ErrFormat
	}
	cmtMax, haveData := int(min(65535, size-22)), 0
	data := make([]byte, 22+cmtMax)

	// If there are fewer than min bytes in the buffer then make it max,
	// not tolerating any errors
	getData := func(min, max int) error {
		if min <= haveData {
			return nil
		}
		if max > len(data) {
			return ErrFormat
		}
		n, err := r.ReadAt(data[len(data)-max:len(data)-haveData], size-int64(max))
		haveData += n
		if haveData != max {
			return err
		}
		return nil
	}
	atNegOffset := func(offset int) byte { return data[len(data)-1-offset] }

	for cmtSize := 0; cmtSize <= cmtMax; cmtSize++ {
		if err := getData(cmtSize+2, cmtSize+22); err != nil {
			return nil, err
		}
		if cmtSize > 0 {
			ch := atNegOffset(cmtSize - 1)
			if ch < 32 && ch != '\t' && ch != '\n' && ch != '\r' {
				return nil, ErrFormat // control chars not allowed in comments
			}
		}
		// Check for 16-bit little-endian comment field
		if atNegOffset(cmtSize) != byte(cmtSize>>8) ||
			atNegOffset(cmtSize+1) != byte(cmtSize) {
			continue
		}
		if err := getData(cmtSize+22, cmtSize+22); err != nil {
			return nil, err
		}
		if atNegOffset(cmtSize+21) == 'P' &&
			atNegOffset(cmtSize+20) == 'K' &&
			atNegOffset(cmtSize+19) == 5 &&
			atNegOffset(cmtSize+18) == 6 {
			return data[len(data)-haveData:], nil
		}
	}
	return nil, ErrFormat
}

const (
	// Unix constants. The specification doesn't mention them,
	// but these seem to be the values agreed on by tools.
	s_IFMT   = 0xf000
	s_IFSOCK = 0xc000
	s_IFLNK  = 0xa000
	s_IFREG  = 0x8000
	s_IFBLK  = 0x6000
	s_IFDIR  = 0x4000
	s_IFCHR  = 0x2000
	s_IFIFO  = 0x1000
	s_ISUID  = 0x800
	s_ISGID  = 0x400
	s_ISVTX  = 0x200

	msdosDir      = 0x10
	msdosReadOnly = 0x01
)

func msdosModeToFileMode(m uint32) (mode fs.FileMode) {
	if m&msdosDir != 0 {
		mode = fs.ModeDir | 0777
	} else {
		mode = 0666
	}
	if m&msdosReadOnly != 0 {
		mode &^= 0222
	}
	return mode
}

func unixModeToFileMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0777)
	switch m & s_IFMT {
	case s_IFBLK:
		mode |= fs.ModeDevice
	case s_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case s_IFDIR:
		mode |= fs.ModeDir
	case s_IFIFO:
		mode |= fs.ModeNamedPipe
	case s_IFLNK:
		mode |= fs.ModeSymlink
	case s_IFREG:
		// nothing to do
	case s_IFSOCK:
		mode |= fs.ModeSocket
	}
	if m&s_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if m&s_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if m&s_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}
