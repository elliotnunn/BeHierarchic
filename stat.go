package main

import (
	"io/fs"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
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
			return sizeDeferredStat{stat, o.container.rapool.ReaderAt(o)}, nil
		} else {
			return stat, nil
		}
	}
}

func (o path) setKnownSize(s int64) {
	stat, err := o.rawStat()
	if err != nil {
		return
	}
	if stat.Mode().IsRegular() && stat.Size() < 0 {
		o.container.rapool.ReaderAt(o).SetSize(s)
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
	fileInfoWithoutSize
	sizer
}

type sizer interface{ Size() int64 }
type fileInfoWithoutSize interface {
	Name() string
	Mode() fs.FileMode
	ModTime() time.Time
	IsDir() bool
	Sys() any
}
