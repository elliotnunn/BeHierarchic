package main

import (
	"fmt"
	"io"
	"io/fs"
	"math"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/spinner"
)

func (fsys *FS) Stat(name string) (stat fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "stat", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	o, err := fsys.path(name)
	if err != nil {
		return nil, err
	}

	return o.cookedStat()
}

func (o path) rawStat() (fs.FileInfo, error) { return fs.Stat(o.fsys, o.name.String()) }
func (o path) cookedStat() (fs.FileInfo, error) {
	// Cases to cover:
	// - a mountpoint: it should not return a name of "."
	// - a file that doesn't know its own size (e.g. a gzip)

	isMountpoint := o.fsys != o.container.root && o.name == internpath.Path{}
	if isMountpoint {
		o.container.rMu.RLock()
		diskImage := o.container.reverse[o.fsys].Thick(o.container)
		o.container.rMu.RUnlock()
		imgStat, err := diskImage.rawStat()
		if err != nil {
			return nil, err
		}
		return mountpointStat{FileInfo: imgStat, name: diskImage.name.Base() + Special}, nil
	} else {
		stat, err := o.rawStat()
		if err != nil {
			return nil, err
		}
		if stat.Mode().IsRegular() && stat.Size() < 0 {
			return sizeDeferredStat{stat, o}, nil
		} else {
			return stat, nil
		}
	}
}

type mountpointStat struct {
	fs.FileInfo // inner
	name        string
}

func (s mountpointStat) Name() string { return s.name }
func (s mountpointStat) IsDir() bool  { return true }
func (s mountpointStat) Mode() fs.FileMode {
	return s.FileInfo.Mode() | fs.ModeDir | s.FileInfo.Mode()&0o444>>2
}

type sizeDeferredStat struct {
	fs.FileInfo // everything but Size()
	o           path
}

func (s sizeDeferredStat) Size() int64 {
	raw, err := s.o.rawStat()
	if err != nil {
		panic(fmt.Sprintf("stat failed where previously stat worked: %v %s", err, s.o))
	}
	if s := raw.Size(); s >= 0 {
		return s
	}

	f, err := s.o.rawOpen()
	if err != nil {
		panic(fmt.Sprintf("open failed where previously stat worked: %v %s", err, s.o))
	}

	if _, randAccess := f.(io.ReaderAt); randAccess {
		panic(fmt.Sprintf("random-access file has unknown size: %v %s", s.o))
	}

	spinner.ReadAt(s.o, make([]byte, 1), math.MaxInt64-1) // read to the end

	raw, err = s.o.rawStat()
	if err != nil {
		panic(fmt.Sprintf("stat failed where previously stat worked: %v %s", err, s.o))
	}
	return raw.Size() // best we can do
}
