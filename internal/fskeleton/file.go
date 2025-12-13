// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
	"testing/iotest"
)

// An Open()ed directory
type file struct {
	id   fileID
	data any
	rd   io.Reader
	cl   io.Closer
}

type rafile struct {
	id fileID
	*io.SectionReader
}

func (f *rafile) Close() error               { return nil }
func (f *rafile) Stat() (fs.FileInfo, error) { return &f.id, nil }

func (f *file) Stat() (fs.FileInfo, error) { return &f.id, nil }
func (f *file) Read(p []byte) (n int, err error) {
	if f.rd == nil {
		switch d := f.data.(type) {
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
		case error:
			f.rd = iotest.ErrReader(d)
		default:
			panic("file.data is not any of our known types")
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
