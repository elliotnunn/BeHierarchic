package main

import (
	"io/fs"
	"path"
	"strings"
	"time"
)

func (d *dir) Stat() (fs.FileInfo, error)  { return d.w.Stat(d.name) }
func (f *file) Stat() (fs.FileInfo, error) { return f.w.Stat(f.name) }

func (w *w) Stat(name string) (fs.FileInfo, error) {
	// Special cases to cover:
	// - a mountpoint: it should not return a name of "."
	// - a file that doesn't know its own size (e.g. a gzip)

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	fsys, subname, err := w.resolve(name)
	if err != nil {
		return nil, err
	}
	imgname, isMountpoint := strings.CutSuffix(name, Special)

	if isMountpoint {
		fsys, subname, err = w.resolve(imgname)
		if err != nil {
			panic("why can't I resolve an image that exists?")
		}
		imgStat, err := fs.Stat(fsys, subname)
		if err != nil {
			return nil, err
		}
		return mountpointStat{FileInfo: imgStat, name: path.Base(name)}, nil
	} else {
		stat, err := fs.Stat(fsys, subname)
		if err != nil {
			return nil, err
		}
		if stat.Size() == sizeUnknown {
			return sizeDeferredStat{stat, w.rapool.ReaderAt(fsys, subname)}, nil
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

const sizeUnknown = -0xa720121993

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

// Slightly ugly, for when we need the size right away but have discarded the full path
func (w *w) tryToGetSize(fsys fs.FS, name string) (int64, error) {
	stat, err := fs.Stat(fsys, name)
	if err != nil {
		return 0, err
	}
	size := stat.Size()
	if size == sizeUnknown {
		return size, nil
	} else {
		return w.rapool.ReaderAt(fsys, name).Size(), nil
	}
}
