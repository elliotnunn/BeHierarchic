// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package resourcefork

import (
	"io"
	"io/fs"
	"time"
)

type rootDir struct {
	fsys       *FS
	listOffset uint16
}

func (*rootDir) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

func (d *rootDir) ReadDir(count int) ([]fs.DirEntry, error) {
	d.fsys.once.Do(d.fsys.parse)
	n := d.fsys.nType - d.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && int(n) > count {
		n = uint16(count)
	}

	list, err := d.fsys.listTypes(d.fsys.resTypeList+2+uint32(d.listOffset)*8, n)
	d.listOffset += uint16(len(list))
	return list, err
}

func (d *rootDir) Stat() (fs.FileInfo, error) {
	return d, nil
}

func (*rootDir) Close() error {
	return nil
}

func (s *rootDir) Name() string { // FileInfo + DirEntry
	return "."
}

func (*rootDir) IsDir() bool { // FileInfo + DirEntry
	return true
}

func (*rootDir) Type() fs.FileMode { // DirEntry
	return fs.ModeDir
}

func (s *rootDir) Info() (fs.FileInfo, error) { // DirEntry
	return s, nil
}

func (*rootDir) Size() int64 { // FileInfo
	return 0
}

func (*rootDir) Mode() fs.FileMode { // FileInfo
	return fs.ModeDir | 0o777
}

func (d *rootDir) ModTime() time.Time { // FileInfo
	return d.fsys.ModTime
}

func (s *rootDir) Sys() any { // FileInfo
	return nil
}
