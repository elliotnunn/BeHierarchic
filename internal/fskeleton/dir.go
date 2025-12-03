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

const implicitDir fs.FileMode = 0xffffffff

var _ node = new(dirent)
var _ fs.ReadDirFile = new(dir) // check satisfies interface

func newDir() *dirent {
	var de dirent
	de.cond.L = &de.mu    // little bit awkward, to relieve heap pressure
	de.mode = implicitDir // directories are born implicit
	// i.e. inferred to exist as a parent of an explicitly created file
	return &de
}

type dirent struct {
	name internpath.Path

	cond sync.Cond
	mu   sync.Mutex

	modtime time.Time
	sys     any
	mode    fs.FileMode

	complete bool // colocate with mode for better alignment
	chs      []node
	chm      map[internpath.Path]uint32
}

type dir struct { // in its open state
	ent        *dirent
	listOffset int
}

func (d *dirent) makeExplicit() {
	if d.mode == implicitDir {
		d.mode = fs.ModeDir
	}
}
func (d *dirent) pathname() internpath.Path { return d.name }
func (d *dirent) open() (fs.File, error)    { return &dir{ent: d, listOffset: 0}, nil }

func (d *dirent) lookup(name internpath.Path) (node, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for {
		if index, ok := d.chm[name]; ok {
			return d.chs[index], nil
		} else if d.complete {
			return nil, fs.ErrNotExist
		}
		d.cond.Wait()
	}
}

// may return fs.ErrExist if a non-dir with this name exists
func (d *dirent) implicitSubdir(name internpath.Path) (*dirent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.complete {
		return nil, fs.ErrPermission
	}

	if index, exist := d.chm[name]; !exist {
		nu := newDir()
		nu.name = name
		if d.chm == nil {
			d.chm = make(map[internpath.Path]uint32)
		}
		d.chm[name] = uint32(len(d.chs))
		d.chs = append(d.chs, nu)
		d.cond.Broadcast()
		return nu, nil
	} else if de, isdir := d.chs[index].(*dirent); isdir {
		return de, nil
	} else {
		return nil, fs.ErrExist
	}
}

// may return fs.ErrExist
func (d *dirent) put(thing node) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.complete {
		return fs.ErrPermission
	}

	if index, exist := d.chm[thing.pathname()]; exist {
		if got, ok := d.chs[index].(*dirent); ok {
			if want, ok := thing.(*dirent); ok {
				return got.replace(want)
			}
		}
		return fs.ErrExist
	}

	if d.chm == nil {
		d.chm = make(map[internpath.Path]uint32)
	}
	d.chm[thing.pathname()] = uint32(len(d.chs))
	d.chs = append(d.chs, thing)
	d.cond.Broadcast()
	return nil
}

func (d *dirent) replace(with *dirent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mode != implicitDir {
		return fs.ErrExist
	}
	d.mode, d.modtime, d.sys = with.mode, with.modtime, with.sys
	d.cond.Broadcast()
	return nil
}

func (d *dirent) noMore(recursive bool) {
	d.mu.Lock()
	d.complete = true
	d.cond.Broadcast()
	d.mu.Unlock()

	for _, c := range d.chs {
		c, ok := c.(*dirent)
		if !ok {
			continue
		}
		c.mu.Lock()
		c.makeExplicit()
		c.cond.Broadcast()
		c.mu.Unlock()
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
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.mode == implicitDir {
		d.cond.Wait()
	}
	return d.mode&^fs.ModeType | fs.ModeDir
}
func (d *dirent) ModTime() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.mode == implicitDir {
		d.cond.Wait()
	}
	return d.modtime
}
func (d *dirent) Sys() any {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.mode == implicitDir {
		d.cond.Wait()
	}
	return d.sys
}

func (*dir) Close() error                 { return nil }
func (*dir) Read([]byte) (int, error)     { return 0, io.EOF }
func (d *dir) Stat() (fs.FileInfo, error) { return d.ent, nil }
func (d *dir) ReadDir(count int) ([]fs.DirEntry, error) {
	d.ent.mu.Lock()
	defer d.ent.mu.Unlock()

	var err error
	if count <= 0 { // "give me everything"
		for !d.ent.complete {
			d.ent.cond.Wait()
		}
		count = len(d.ent.chs) - d.listOffset
		err = nil
	} else { // "give me up to count"
		for !d.ent.complete && len(d.ent.chs) <= d.listOffset {
			d.ent.cond.Wait()
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
