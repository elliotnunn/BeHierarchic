package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/spinner"
)

func (fsys *FS) Open(name string) (fs.File, error) {
	// Cases to cover:
	// - all files must implement io.ReaderAt
	// - all directories must have mountpoints added to their listing
	name, suppressSpecialSiblings := checkAndDeleteComponent(name, ".nodeeper")

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	subsys, subname, err := fsys.resolve(name)
	if err != nil {
		return nil, err
	}

	f, err := subsys.Open(subname.String())
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
		if _, supportsRandomAccess := f.(io.ReaderAt); !supportsRandomAccess {
			f = &file{fsys: fsys, name: name, obj: f, rdr: fsys.rapool.ReaderAt(reopenableFile{fsys, key{subsys, subname}})}
		}
	case fs.ModeDir:
		if !suppressSpecialSiblings {
			f = &dir{obj: f.(fs.ReadDirFile), fsys: fsys, name: name}
		}
	}
	return f, nil
}

type dir struct {
	obj   fs.ReadDirFile
	fsys  *FS
	name  string
	list  []fs.DirEntry
	lseek int
}

func (d *dir) Close() error               { return d.obj.Close() }
func (d *dir) Read(p []byte) (int, error) { return 0, io.EOF }

type file struct {
	name string
	fsys *FS
	obj  fs.File
	rdr  spinner.ReaderAt
	seek int64
}

func (f *file) Close() error                            { return f.obj.Close() }
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
			return path.Join(l2...), true
		}
	} else {
		return name, false
	}
}
