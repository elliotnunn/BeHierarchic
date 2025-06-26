// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package resourcefork

import (
	"encoding/binary"
	"io"
	"io/fs"
	"strconv"
	"sync"
	"time"
)

type resourceFile struct {
	fsys   *FS
	offset uint32
	id     int16
	once   sync.Once
	reader *io.SectionReader
}

func (f *resourceFile) initReader() {
	f.once.Do(func() {
		var s [4]byte
		_, err := f.fsys.AppleDouble.ReadAt(s[:], int64(f.offset))
		if err != nil {
			panic("unreadable resource")
		}
		f.reader = io.NewSectionReader(f.fsys.AppleDouble,
			int64(f.offset)+4,
			int64(binary.BigEndian.Uint32(s[:])))
	})
}

func (f *resourceFile) Read(p []byte) (n int, err error) {
	f.initReader()
	return f.reader.Read(p)
}

func (f *resourceFile) ReadAt(p []byte, off int64) (n int, err error) {
	f.initReader()
	return f.reader.ReadAt(p, off)
}

func (f *resourceFile) Seek(offset int64, whence int) (int64, error) {
	f.initReader()
	return f.reader.Seek(offset, whence)
}

func (f *resourceFile) Stat() (fs.FileInfo, error) {
	return f, nil
}

func (f *resourceFile) Close() error {
	return nil
}

func (f *resourceFile) Name() string { // FileInfo + DirEntry
	return strconv.Itoa(int(f.id))
}

func (f *resourceFile) IsDir() bool { // FileInfo + DirEntry
	return false
}

func (f *resourceFile) Type() fs.FileMode { // DirEntry
	return 0
}

func (f *resourceFile) Info() (fs.FileInfo, error) { // DirEntry
	return f, nil
}

func (f *resourceFile) Size() int64 { // FileInfo
	f.initReader()
	return f.reader.Size()
}

func (f *resourceFile) Mode() fs.FileMode { // FileInfo
	return 0o666
}

func (f *resourceFile) ModTime() time.Time { // FileInfo
	return f.fsys.ModTime
}

func (f *resourceFile) Sys() any { // FileInfo
	return nil
}
