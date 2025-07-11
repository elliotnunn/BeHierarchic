// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package reader2readerat

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"weak"
)

type FS struct {
	FS    fs.FS
	reuse map[string]keeptrack
	lock  sync.Mutex
}

type File struct {
	ra   *ReaderAt // Close() nils this to prevent double-close
	arch *FS
	name string
	seek int64
	stat fs.FileInfo
}

type keeptrack struct {
	refcnt uintptr
	ra     *ReaderAt
}

type guarantee interface {
	io.ReaderAt
	io.Seeker
}

type unique struct {
	fsys weak.Pointer[fs.FS]
	path string
}

// If opening a file, guaranteed to satisfy io.ReaderAt and io.Seeker
func (r *FS) Open(name string) (fs.File, error) {
	f, err := r.FS.Open(name)
	if err != nil {
		return nil, err
	}

	if _, ok := f.(guarantee); ok { // already seekable, nothing to add here
		return f, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("unable to stat an open zip file: %w", err)
	}
	if stat.IsDir() {
		return f, err
	}

	defer f.Close() // odd, I know, but bear with me...
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.reuse == nil {
		r.reuse = make(map[string]keeptrack)
	}

	saved, ok := r.reuse[name]
	if !ok {
		unique := unique{
			fsys: weak.Make(&r.FS),
			path: name,
		}
		reopener := func() (io.Reader, error) {
			return r.FS.Open(name)
		}
		saved = keeptrack{ra: NewFromReader(unique, reopener)}
	}
	saved.refcnt++
	r.reuse[name] = saved

	return &File{
		arch: r,
		name: name,
		ra:   saved.ra,
		seek: 0,
		stat: stat,
	}, nil
}

func (f *File) ReadAt(buf []byte, off int64) (n int, err error) {
	return f.ra.ReadAt(buf, off)
}

func (f *File) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.seek)
	f.seek += int64(n)
	return n, err
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.seek
	case io.SeekEnd:
		offset += f.stat.Size()
	default:
		return 0, errWhence
	}
	if offset < 0 {
		return 0, errOffset
	}
	f.seek = offset
	return offset, nil
}

func (f *File) Stat() (fs.FileInfo, error) {
	return f.stat, nil
}

func (f *File) Size() int64 {
	return f.stat.Size()
}

func (f *File) Close() error {
	if f.ra == nil {
		return fs.ErrClosed
	}
	var err error
	f.arch.lock.Lock()
	defer f.arch.lock.Unlock()
	saved := f.arch.reuse[f.name]
	saved.refcnt--
	if saved.refcnt == 0 {
		err = saved.ra.Close()
		delete(f.arch.reuse, f.name)
	} else {
		f.arch.reuse[f.name] = saved
	}
	f.ra = nil
	return err
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")
