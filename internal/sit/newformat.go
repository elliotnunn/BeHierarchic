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

func newFormat(fsys *fskeleton.FS, disk io.ReaderAt, offset int64) {
	defer fsys.NoMore()
	var (
		pass2 []file
		known = make(map[int64]file)
	)
	for {
		f, err := headers(disk, offset)
		err = cvtEOF(err)
		if err == io.EOF {
			break
		} else if err != nil {
			slog.Warn("StuffIt read error", "err", err, "offset", offset)
			return
		}

		ok := addToFS(fsys, f, disk, known)
		if !ok {
			pass2 = append(pass2, f)
		}
		offset = f.Next()
	}

	for _, f := range pass2 {
		addToFS(fsys, f, disk, known)
	}
}

func addToFS(fsys *fskeleton.FS, f file, disk io.ReaderAt, known map[int64]file) bool {
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
		fsys.CreateDir(name, 0, meta.ModTime, nil)
		adfile, adlen := meta.ForDir()
		fsys.CreateSequentialFile(appledouble.Sidecar(name), 0, adfile, adlen, 0, meta.ModTime, nil)
	} else { // file
		rOffset := f.HeaderEnd
		if macstuff.Rsrc.Algo == 0 && f.RCrypt == "" {
			adfile, adsize := meta.WithResourceFork(
				io.NewSectionReader(disk, rOffset, int64(macstuff.Rsrc.Unpacked)),
				int64(macstuff.Rsrc.Unpacked))
			fsys.CreateRandomAccessFile(appledouble.Sidecar(name),
				f.HeaderEnd,          // order
				adfile,               // reader
				adsize,               // size
				0, meta.ModTime, nil) // mode, mtime, sys
		} else {
			adfile, adsize := meta.WithSequentialResourceFork(func() io.Reader {
				return readerFor(macstuff.Rsrc.Algo, f.RCrypt, macstuff.Rsrc.Unpacked, macstuff.Rsrc.CRC,
					io.NewSectionReader(disk, rOffset, int64(macstuff.Rsrc.Packed)))
			}, int64(macstuff.Rsrc.Unpacked))
			fsys.CreateSequentialFile(appledouble.Sidecar(name),
				f.HeaderEnd,          // order
				adfile,               // reader
				adsize,               // size
				0, meta.ModTime, nil) // mode, mtime, sys
		}

		dOffset := f.HeaderEnd + int64(macstuff.Rsrc.Packed)
		if f.Common.Data.Algo == 0 && f.DCrypt == "" {
			fsys.CreateRandomAccessFile(name,
				f.HeaderEnd+1, // order
				io.NewSectionReader(disk, dOffset, int64(f.Common.Data.Unpacked)), // readerAt
				int64(f.Common.Data.Unpacked),                                     // size
				0, meta.ModTime, nil)                                              // mode, mtime, sys
		} else {
			fsys.CreateSequentialFile(name,
				f.HeaderEnd+1, // order
				func() io.Reader {
					return readerFor(f.Common.Data.Algo, f.DCrypt, f.Common.Data.Unpacked, f.Common.Data.CRC,
						io.NewSectionReader(disk, dOffset, int64(f.Common.Data.Packed)))
				}, // reader
				int64(f.Common.Data.Unpacked), // size
				0, meta.ModTime, nil)          // mode, mtime, sys
		}
	}

	return true
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
		err = ErrFormat
		return
	}
	structsize := int(binary.BigEndian.Uint16(buf[6:]))
	if structsize < 48 {
		err = ErrFormat
		return
	}
	buf, err = creepTo(buf, reader, structsize)
	if err != nil {
		return
	}

	// Checksum that encompasses both of these
	if !checkCRC16(buf, 32) {
		err = ErrChecksum
		return
	}

	binary.Read(bytes.NewReader(buf), binary.BigEndian, &f.Common)

	// The arbitrary bytes of encryption info (for the data fork)
	tail := buf[48:]
	if f.Common.Typ&0x40 == 0 {
		if len(tail) < int(f.Common.Data.CryptBytes) {
			err = ErrFormat
			return
		}
		f.DCrypt = string(tail[:f.Common.Data.CryptBytes])
		tail = tail[f.Common.Data.CryptBytes:]
	}

	// The arbitrary name bytes
	if len(tail) < int(f.Common.NameLen) {
		err = ErrFormat
		return
	}
	f.Name = string(tail[:f.Common.NameLen])
	tail = tail[f.Common.NameLen:]

	// The comment (seems to be an afterthought)
	if len(tail) > 4 {
		commentsize := int(binary.BigEndian.Uint16(tail))
		tail = tail[4:]
		if commentsize > len(tail) {
			err = ErrFormat
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
			err = ErrChecksum
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
			err = ErrChecksum
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
