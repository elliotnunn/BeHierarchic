// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

var _ node = new(dirent)
var _ fs.ReadDirFile = new(dir) // check satisfies interface

func newDir() *dirent {
	var de dirent
	de.iCond.L = &de.iMu // little bit awkward, to relieve heap pressure
	de.cCond.L = &de.cMu
	de.chm = make(map[internpath.Path]node)
	return &de
}

type dirent struct {
	name internpath.Path

	iCond   sync.Cond
	iMu     sync.Mutex
	iOK     bool
	mode    fs.FileMode
	modtime time.Time
	sys     any

	cCond    sync.Cond
	cMu      sync.Mutex
	complete bool
	chs      []node
	chm      map[internpath.Path]node
}

type dir struct { // in its open state
	ent        *dirent
	listOffset int
}

func (d *dirent) pathname() internpath.Path { return d.name }
func (d *dirent) open() (fs.File, error)    { return &dir{ent: d, listOffset: 0}, nil }

func (d *dirent) lookup(name internpath.Path) (node, error) {
	d.cMu.Lock()
	defer d.cMu.Unlock()
	for {
		if got, ok := d.chm[name]; ok {
			return got, nil
		} else if d.complete {
			return nil, fs.ErrNotExist
		}
		d.cCond.Wait()
	}
}

// may return fs.ErrExist if a non-dir with this name exists
func (d *dirent) implicitSubdir(name internpath.Path) (*dirent, error) {
	d.cMu.Lock()
	defer d.cMu.Unlock()
	if d.complete {
		return nil, fs.ErrPermission
	}

	if got, exist := d.chm[name]; !exist {
		got := newDir()
		got.name = name
		d.chm[name] = got
		d.chs = append(d.chs, got)
		d.cCond.Broadcast()
		return got, nil
	} else if de, isdir := got.(*dirent); isdir {
		return de, nil
	} else {
		return nil, fs.ErrExist
	}
}

// may return fs.ErrExist
func (d *dirent) put(thing node) error {
	d.cMu.Lock()
	defer d.cMu.Unlock()
	if d.complete {
		return fs.ErrPermission
	}

	if got, exist := d.chm[thing.pathname()]; exist {
		if got, ok := got.(*dirent); ok {
			if want, ok := thing.(*dirent); ok {
				return got.replace(want)
			}
		}
		return fs.ErrExist
	}

	d.chm[thing.pathname()] = thing
	d.chs = append(d.chs, thing)
	d.cCond.Broadcast()
	return nil
}

func (d *dirent) replace(with *dirent) error {
	d.iMu.Lock()
	defer d.iMu.Unlock()
	if d.iOK {
		return fs.ErrExist
	}
	d.mode, d.modtime, d.sys = with.mode, with.modtime, with.sys
	d.iOK = true
	d.iCond.Broadcast()
	return nil
}

func (d *dirent) noMore(recursive bool) {
	d.cMu.Lock()
	d.complete = true
	d.cCond.Broadcast()
	d.cMu.Unlock()

	for _, c := range d.chs {
		c, ok := c.(*dirent)
		if !ok {
			continue
		}
		c.iMu.Lock()
		c.iOK = true
		c.iCond.Broadcast()
		c.iMu.Unlock()
		if recursive {
			c.noMore(true)
		}
	}
}

// common to fs.DirEntry and fs.FileInfo
func (d *dirent) Name() string { return d.name.Base() }
func (d *dirent) IsDir() bool  { return true }

// fs.DirEntry
func (d *dirent) Type() fs.FileMode          { return fs.ModeDir }
func (d *dirent) Info() (fs.FileInfo, error) { return d, nil }

// fs.FileInfo
func (d *dirent) Size() int64 { return 0 }
func (d *dirent) Mode() fs.FileMode {
	d.iMu.Lock()
	defer d.iMu.Unlock()
	for !d.iOK {
		d.iCond.Wait()
	}
	return d.mode&^fs.ModeType | fs.ModeDir
}
func (d *dirent) ModTime() time.Time {
	d.iMu.Lock()
	defer d.iMu.Unlock()
	for !d.iOK {
		d.iCond.Wait()
	}
	return d.modtime
}
func (d *dirent) Sys() any {
	d.iMu.Lock()
	defer d.iMu.Unlock()
	for !d.iOK {
		d.iCond.Wait()
	}
	return d.sys
}

func (*dir) Close() error                 { return nil }
func (*dir) Read([]byte) (int, error)     { return 0, io.EOF }
func (d *dir) Stat() (fs.FileInfo, error) { return d.ent, nil }
func (d *dir) ReadDir(count int) ([]fs.DirEntry, error) {
	d.ent.cMu.Lock()
	defer d.ent.cMu.Unlock()

	var err error
	if count <= 0 { // "give me everything"
		for !d.ent.complete {
			d.ent.cCond.Wait()
		}
		count = len(d.ent.chs) - d.listOffset
		err = nil
	} else { // "give me up to count"
		for !d.ent.complete && len(d.ent.chs) <= d.listOffset {
			d.ent.cCond.Wait()
		}
		count = min(count, len(d.ent.chs)-d.listOffset)
		if d.ent.complete && len(d.ent.chs) == d.listOffset+count {
			err = io.EOF
		} else {
			err = nil
		}
	}

	list := make([]fs.DirEntry, count)
	for i := range list {
		list[i] = d.ent.chs[d.listOffset+i]
	}
	d.listOffset += count
	return list, err
}
