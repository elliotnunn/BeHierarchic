package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	gopath "path"
	"reflect"
	"strings"

	bufra "github.com/avvmoto/buf-readerat"
	"github.com/elliotnunn/BeHierarchic/internal/spinner"
)

func (fsys *FS) Open(name string) (f fs.File, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "open", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	o, err := fsys.path(name)
	if err != nil {
		return nil, err
	}

	return o.cookedOpen()
}

func (o path) rawOpen() (fs.File, error) { return o.fsys.Open(o.name.String()) }
func (o path) cookedOpen() (fs.File, error) {
	// Cases to cover:
	// - all files must implement io.ReaderAt
	// - syscalls to os.File are slow, so buffer them
	// - all directories must have mountpoints added to their listing
	f, err := o.rawOpen()
	if err != nil {
		return nil, err // would be nice to make this more informative
	}

	s, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("unexpectedly unable to stat an open file: %w", err)
	}

	switch s.Mode() & fs.ModeType {
	case 0: // regular file
		if osFile, ok := f.(*os.File); ok {
			withBuffer := bufra.NewBufReaderAt(osFile, 1024) // untuned buffer size
			f = osFileBuffered{
				allReadMethods: io.NewSectionReader(withBuffer, 0, s.Size()),
				statCloser:     f}
		} else if _, supportsRandomAccess := f.(io.ReaderAt); !supportsRandomAccess {
			f.Close()
			ra := o.container.rapool.ReaderAt(o)
			if size := s.Size(); size >= 0 {
				ra.SetSize(size) // useful for the spinner to know the size
			}
			f = &file{path: o, rdr: ra}
		}
	case fs.ModeDir:
		rd, ok := f.(fs.ReadDirFile)
		if ok {
			f = &dir{path: o, obj: rd}
		} else {
			slog.Warn("dirWithoutReadDir",
				"path", o.String(),
				"type", reflect.TypeOf(f).Name(),
				"mode", s.Mode().String())
		}
	}
	return f, nil
}

type statCloser interface {
	Stat() (fs.FileInfo, error)
	Close() error
}

type allReadMethods interface {
	io.ReadSeeker
	io.ReaderAt
}

type osFileBuffered struct {
	statCloser
	allReadMethods
}

type dir struct {
	path  path
	obj   fs.ReadDirFile
	list  []fs.DirEntry
	lseek int
}

func (d *dir) Stat() (fs.FileInfo, error) { return d.path.cookedStat() }
func (d *dir) Close() error               { return d.obj.Close() }
func (d *dir) Read(p []byte) (int, error) { return 0, io.EOF }

type file struct {
	path path
	rdr  spinner.ReaderAt
	seek int64
}

func (f *file) Stat() (fs.FileInfo, error)              { return f.path.cookedStat() }
func (f *file) Close() error                            { return nil }
func (f *file) ReadAt(p []byte, off int64) (int, error) { return f.rdr.ReadAt(p, off) }

func (f *file) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.seek)
	f.seek += int64(n)
	return n, err
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.seek
	case io.SeekEnd:
		offset += f.rdr.Size() // could be costly
	default:
		return 0, errWhence
	}
	if offset < 0 {
		return 0, errOffset
	}
	f.seek = offset
	return offset, nil
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")

func checkAndDeleteComponent(name string, special string) (string, bool) {
	foundSpecial := false
	l := strings.Split(name, "/")
	var l2 []string
	for _, s := range l {
		if s == special {
			foundSpecial = true
		} else {
			l2 = append(l2, s)
		}
	}
	if foundSpecial {
		if len(l2) == 0 {
			return ".", true
		} else {
			return gopath.Join(l2...), true
		}
	} else {
		return name, false
	}
}
