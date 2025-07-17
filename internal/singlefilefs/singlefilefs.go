// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package singlefilefs

import (
	"io"
	"io/fs"
	"sync"
	"time"
)

// Single-file archive
type FS struct {
	Name       string
	FileOpener func() (io.Reader, error)
	ModTime    time.Time
	Size       int64 // set to negative to calculate size by reading whole file
	once       sync.Once
}

type Dir struct {
	fsys     *FS
	listDone bool
}

type File struct {
	fsys     *FS
	data     io.Reader
	calcSize int64
}

func (fsys *FS) Open(name string) (fs.File, error) {
	switch name {
	default:
		return nil, fs.ErrNotExist
	case ".":
		return &Dir{fsys: fsys}, nil
	case fsys.Name:
		return &File{fsys: fsys}, nil
	}
}

func (d *Dir) Read(p []byte) (n int, err error) {
	return 0, fs.ErrInvalid
}

func (d *Dir) Stat() (fs.FileInfo, error) {
	return d, nil
}

func (d *Dir) Close() error {
	return nil
}

func (d *Dir) ReadDir(count int) ([]fs.DirEntry, error) {
	if d.listDone {
		return nil, io.EOF
	} else {
		d.listDone = true
		return []fs.DirEntry{&File{fsys: d.fsys}}, nil
	}
}

func (f *File) Read(p []byte) (n int, err error) {
	if f.data == nil {
		var err error
		f.data, err = f.fsys.FileOpener()
		if err != nil {
			return 0, err
		}
	}
	return f.data.Read(p)
}

func (f *File) Stat() (fs.FileInfo, error) {
	return f, nil
}

func (f *File) Close() error {
	if closer, ok := f.data.(io.Closer); ok {
		return closer.Close()
	} else {
		return nil
	}
}

func (f *File) Size() int64 {
	f.fsys.once.Do(func() {
		if f.fsys.Size >= 0 {
			return
		}

		f.fsys.Size = 0

		o, err := f.fsys.FileOpener()
		if err != nil {
			return
		}
		if closer, ok := o.(io.Closer); ok {
			defer closer.Close()
		}

		for {
			var buf [16 * 1024]byte
			n, err := o.Read(buf[:])
			f.fsys.Size += int64(n)
			if err != nil {
				break
			}
		}
	})
	return f.fsys.Size
}

func (f *File) Name() string {
	return f.fsys.Name
}
func (f *File) Mode() fs.FileMode {
	return 0o644
}
func (f *File) Type() fs.FileMode {
	return 0 // regular file
}
func (f *File) Info() (fs.FileInfo, error) {
	return f, nil
}
func (f *File) ModTime() time.Time {
	return f.fsys.ModTime
}
func (f *File) IsDir() bool {
	return false
}
func (f *File) Sys() any {
	return nil
}

func (d *Dir) Name() string {
	return "."
}
func (d *Dir) Size() int64 {
	return 0
}
func (d *Dir) Mode() fs.FileMode {
	return 0o755 | fs.ModeDir
}
func (d *Dir) ModTime() time.Time {
	return d.fsys.ModTime
}
func (d *Dir) IsDir() bool {
	return true
}
func (d *Dir) Sys() any {
	return nil
}
