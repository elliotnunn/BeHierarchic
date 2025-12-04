// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

var _ node = new(fileent) // check satisfies interface

type fileent struct {
	name    internpath.Path
	order   int64
	size    int64
	mode    fs.FileMode
	modtime int64
	sys     any
	data    any // io.ReaderAt or func() (io.Reader, error) or func() (io.ReadCloser, error)
}

type file struct {
	ent *fileent
	rd  io.Reader
	cl  io.Closer
}

type rafile struct {
	ent *fileent
	*io.SectionReader
}

func (f *fileent) open() (fs.File, error) {
	switch d := f.data.(type) {
	case io.ReaderAt:
		return &rafile{
			ent:           f,
			SectionReader: io.NewSectionReader(d, 0, f.size),
		}, nil
	default:
		return &file{
			ent: f,
		}, nil
	}
}

func (f *fileent) pathname() internpath.Path { return f.name }

// common to fs.DirEntry and fs.FileInfo
func (f *fileent) Name() string { return f.name.Base() }
func (f *fileent) IsDir() bool  { return false }

// fs.DirEntry
func (f *fileent) Type() fs.FileMode          { return 0 }
func (f *fileent) Info() (fs.FileInfo, error) { return f, nil }

// fs.FileInfo
func (f *fileent) Size() int64        { return f.size }
func (f *fileent) Mode() fs.FileMode  { return f.mode &^ fs.ModeType }
func (f *fileent) ModTime() time.Time { return timeToStdlib(f.modtime) }
func (f *fileent) Sys() any           { return f.sys }

// extension to fs.FileInfo
func (f *fileent) Order() int64 { return f.order }

// fs.File
func (f *rafile) Stat() (fs.FileInfo, error) { return f.ent, nil }
func (*rafile) Close() error                 { return nil }

// optimisation to strip the seek info
func (f *rafile) Outer() (r io.ReaderAt, offset int64, n int64) {
	return f.ent.data.(io.ReaderAt), 0, f.ent.size
}

func (f *file) Stat() (fs.FileInfo, error) { return f.ent, nil }
func (f *file) Read(p []byte) (n int, err error) {
	if f.rd == nil {
		switch d := f.ent.data.(type) {
		case func() (io.Reader, error):
			f.rd, err = d()
			if err != nil {
				return n, err
			}
		case func() (io.ReadCloser, error):
			reader, err := d()
			if err != nil {
				return n, err
			}
			f.rd, f.cl = reader, reader
		}
	}
	return f.rd.Read(p)
}

func (f *file) Close() error {
	if f.cl == nil {
		return nil
	} else {
		return f.cl.Close()
	}
}
