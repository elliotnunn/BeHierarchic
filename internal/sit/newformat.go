package sit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/sectionreader"
)

type file struct {
	Offset         int64
	Common         commonHeader
	Comment        string
	OS             any    // headerMac or headerWin or nil
	DCrypt, RCrypt string // the arbitrary password data
	Name           string
	HeaderEnd      int64
}

type commonHeader struct {
	Magic           uint32
	OS              uint8 // 1=Mac 3=Win (so I guess 2=Solaris?)
	_               uint8
	HdrSize         uint16
	_               uint8
	Typ             uint8
	CrTime, ModTime uint32

	Prev, Next, Parent uint32

	NameLen uint16
	HdrCRC  uint16
	Data    fork
}

// 32 bytes, no clue what it does
type headerWin struct {
	_      uint16
	HdrCRC uint16
	_      [14]uint16
}

// 36 or (50 + rsrcfork-password-info) bytes
type headerMac struct {
	Flags      uint16 // 1 = hasResourceFork
	HdrCRC     uint16
	FinderInfo [16]byte
	_          [16]byte
	Rsrc       fork
}

type fork struct {
	Unpacked   uint32 // for dirs, is offset of first entry... also can be FFFFFFFF
	Packed     uint32 // for dirs, is "size of complete directory"
	CRC        uint16
	_          uint8 // usually but not always the same as CryptBytes
	_          uint8 // 0, 0x60, 0x6f
	Algo       AlgID
	CryptBytes uint8 // for dirs, is the number of children
}

func (h *commonHeader) IsDir() bool { return h.Typ&0x40 != 0 }

func (f *file) Next() int64 {
	next := f.HeaderEnd
	if mac, ok := f.OS.(headerMac); ok {
		next += int64(mac.Rsrc.Packed)
	}
	if !f.Common.IsDir() {
		next += int64(f.Common.Data.Packed)
	}
	return next
}

func newFormat(fsys *fskeleton.FS, headerReader, dataReader io.ReaderAt, offset, filesize int64) {
	defer fsys.NoMore()
	var (
		pass2 []file
		known = make(map[int64]file)
	)
	for offset < filesize {
		f, err := headers(headerReader, offset)
		err = notExpectingEOF(err)
		if err != nil {
			slog.Warn("StuffIt read error", "err", err, "offset", offset)
			break
		}

		ok := addToFS(fsys, f, dataReader, known)
		if !ok {
			pass2 = append(pass2, f)
		}
		offset = f.Next()
	}

	for _, f := range pass2 {
		addToFS(fsys, f, dataReader, known)
	}
}

func addToFS(fsys *fskeleton.FS, f file, dataReader io.ReaderAt, known map[int64]file) bool {
	known[f.Offset] = f

	if f.Name == "" {
		return true // it's an end-of-directory marker, ignore it
	}

	name, ok := nameFromThreads(f.Offset, known)
	if !ok {
		return false
	}

	var meta appledouble.AppleDouble
	meta.CreateTime = appledouble.MacTime(f.Common.CrTime)
	meta.ModTime = appledouble.MacTime(f.Common.ModTime)
	meta.Comment = f.Comment

	var macstuff headerMac
	if m, ok := f.OS.(headerMac); ok {
		macstuff = m
	}
	meta.LoadFInfo(&macstuff.FinderInfo)

	if f.Common.IsDir() {
		fsys.Mkdir(name, fileID(f.Offset, false), 0, meta.ModTime)
		adfile, adlen := meta.ForDir()
		fsys.CreateReader(appledouble.Sidecar(name), fileID(f.Offset, true), adfile, adlen, 0, meta.ModTime)
	} else { // file
		rOffset := f.HeaderEnd
		if macstuff.Rsrc.Algo == 0 && f.RCrypt == "" {
			adfile, adsize := meta.WithResourceFork(
				sectionreader.Section(dataReader, rOffset, int64(macstuff.Rsrc.Unpacked)),
				int64(macstuff.Rsrc.Unpacked))
			fsys.CreateReaderAt(appledouble.Sidecar(name),
				fileID(f.Offset, true),
				adfile, adsize, 0, meta.ModTime)
		} else {
			adfile, adsize := meta.WithSequentialResourceFork(func() (io.ReadCloser, error) {
				return readerFor(macstuff.Rsrc.Algo, f.RCrypt, macstuff.Rsrc.Unpacked, macstuff.Rsrc.CRC,
					io.NewSectionReader(dataReader, rOffset, int64(macstuff.Rsrc.Packed)))
			}, int64(macstuff.Rsrc.Unpacked))
			fsys.CreateReadCloser(appledouble.Sidecar(name),
				fileID(f.Offset, true),
				adfile, adsize, 0, meta.ModTime)
		}

		dOffset := f.HeaderEnd + int64(macstuff.Rsrc.Packed)
		if f.Common.Data.Algo == 0 && f.DCrypt == "" {
			fsys.CreateReaderAt(name,
				fileID(f.Offset, false),
				sectionreader.Section(dataReader, dOffset, int64(f.Common.Data.Unpacked)), // readerAt
				int64(f.Common.Data.Unpacked), 0, meta.ModTime)
		} else {
			fsys.CreateReadCloser(name,
				fileID(f.Offset, false),
				func() (io.ReadCloser, error) {
					return readerFor(f.Common.Data.Algo, f.DCrypt, f.Common.Data.Unpacked, f.Common.Data.CRC,
						io.NewSectionReader(dataReader, dOffset, int64(f.Common.Data.Packed)))
				}, // reader
				int64(f.Common.Data.Unpacked), 0, meta.ModTime)
		}
	}

	return true
}

// fileID returns a durable identifier
func fileID(headerOffset int64, isAppleDouble bool) int64 {
	n := headerOffset << 1
	if !isAppleDouble {
		n |= 1 // because resource forks come before data forks
	}
	return n
}

// Tricky and delicate task to read this variable-sized data structure
func headers(r io.ReaderAt, offset int64) (f file, err error) {
	f.Offset = offset

	// The basic common buffer
	reader := io.NewSectionReader(r, offset, math.MaxInt64)
	buf, err := creepTo(nil, reader, 8)
	if err != nil {
		return
	} else if string(buf[:4]) != "\xA5\xA5\xA5\xA5" {
		err = ErrHeader
		return
	}
	structsize := int(binary.BigEndian.Uint16(buf[6:]))
	if structsize < 48 {
		err = ErrHeader
		return
	}
	buf, err = creepTo(buf, reader, structsize)
	if err != nil {
		return
	}

	// Checksum that encompasses both of these
	if !checkCRC16(buf, 32) {
		err = ErrHeader
		return
	}

	binary.Read(bytes.NewReader(buf), binary.BigEndian, &f.Common)

	// The arbitrary bytes of encryption info (for the data fork)
	tail := buf[48:]
	if f.Common.Typ&0x40 == 0 {
		if len(tail) < int(f.Common.Data.CryptBytes) {
			err = ErrHeader
			return
		}
		f.DCrypt = string(tail[:f.Common.Data.CryptBytes])
		tail = tail[f.Common.Data.CryptBytes:]
	}

	// The arbitrary name bytes
	if len(tail) < int(f.Common.NameLen) {
		err = ErrHeader
		return
	}
	f.Name = string(tail[:f.Common.NameLen])
	tail = tail[f.Common.NameLen:]

	// The comment (seems to be an afterthought)
	if len(tail) > 4 {
		commentsize := int(binary.BigEndian.Uint16(tail))
		tail = tail[4:]
		if commentsize > len(tail) {
			err = ErrHeader
			return
		}
		f.Comment = string(tail[:commentsize])
	}

	// Now reset the accumulator and read the OS-specific header
	// Is there even going to be an OS-specific header?
	f.HeaderEnd = f.Offset + int64(len(buf))
	if f.Common.NameLen == 0 {
		return
	}

	switch f.Common.OS {
	case 3: // windows
		buf = nil
		buf, err = creepTo(buf, reader, 32)
		if err != nil {
			return
		}
		if !checkCRC16(buf, 2) {
			err = ErrHeader
			return
		}
		var h2win headerWin
		binary.Read(bytes.NewReader(buf), binary.BigEndian, &h2win)
		f.OS = h2win
		f.HeaderEnd += int64(len(buf))
		return
	case 1: // macintosh
		buf = nil
		buf, err = creepTo(buf, reader, 36)
		if err != nil {
			return
		}
		if buf[1]&1 != 0 { // we have a resource fork
			buf, err = creepTo(buf, reader, 50)
			if err != nil {
				return
			}
			// More arbitrary bytes of encryption info
			if buf[49] != 0 {
				buf, err = creepBy(buf, reader, int(buf[49]))
				if err != nil {
					return
				}
				f.RCrypt = string(buf[50:])
			}
		}
		if !checkCRC16(buf, 2) {
			err = ErrHeader
			return
		}
		var h2mac headerMac
		binary.Read(bytes.NewReader(buf), binary.BigEndian, &h2mac)
		f.OS = h2mac
		f.HeaderEnd += int64(len(buf))
		return
	default:
		err = fmt.Errorf("unknown OS code: %d", f.Common.OS)
		return
	}
}

func nameFromThreads(o int64, known map[int64]file) (string, bool) {
	var s string
	for o != 0 {
		f, ok := known[o]
		if !ok {
			return "", false
		}
		s = path.Join(f.Name, s)
		o = int64(f.Common.Parent)
	}
	return s, true
}
