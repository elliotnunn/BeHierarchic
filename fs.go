// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/spinner"
)

const Special = "◆"

type FS struct {
	bMu     sync.RWMutex
	burrows map[key]*b

	root   fs.FS
	rapool *spinner.Pool
}

type key struct {
	fsys fs.FS
	name string
}

type b struct {
	// nil          = not sure yet
	// notAnArchive = not an archive
	// func()       = archive creator-function
	// fs.FS        = FS
	lock sync.Mutex
	data any
}

type notAnArchive struct{}
type fsysGenerator func(io.ReaderAt) (fs.FS, error)

func Wrapper(fsys fs.FS) *FS {
	const blockShift = 13 // 8 kb
	return &FS{
		root:    fsys,
		burrows: make(map[key]*b),
		rapool:  spinner.New(blockShift, memLimit>>blockShift, 200 /*open readers at once*/),
	}
}

func (fsys *FS) resolve(name string) (subsys fs.FS, subname string, err error) {
	warps := strings.Split(name, Special+"/")
	if strings.HasSuffix(name, Special) {
		warps[len(warps)-1] = strings.TrimSuffix(warps[len(warps)-1], Special)
		warps = append(warps, ".")
	}
	warps, name = warps[:len(warps)-1], warps[len(warps)-1]

	subsys = fsys.root
	for _, el := range warps {
		subsubsys, err := fsys.whatArchive(subsys, el)
		if err != nil {
			return nil, "", err
		} else if subsubsys == nil {
			return nil, "", fs.ErrNotExist
		}
		subsys = subsubsys
	}
	return subsys, name, nil
}

func instantiate(generator fsysGenerator, converter *spinner.Pool, fsys fs.FS, name string) (fs.FS, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	r, nativeReaderAt := f.(io.ReaderAt)
	if !nativeReaderAt {
		f.Close()
		f = nil
		r = converter.ReaderAt(fsys, name)
	}
	fsys2, err := generator(r)
	if err != nil {
		if f != nil {
			f.Close()
		}
		return nil, err
	}
	return fsys2, nil
}

func (fsys *FS) isArchive(subsys fs.FS, subname string) (bool, error) {
	b := fsys.getB(subsys, subname)
	b.lock.Lock()
	defer b.lock.Unlock()

	switch b.data.(type) {
	case notAnArchive:
		return false, nil
	case fsysGenerator, fs.FS:
		return true, nil
	default: // not yet decided
		gen, err := fsys.probeArchive(subsys, subname)
		if err != nil {
			return false, err
		}
		if gen == nil {
			b.data = notAnArchive{}
			return false, nil
		} else {
			b.data = gen
			return true, nil
		}
	}
}

func (fsys *FS) whatArchive(subsys fs.FS, subname string) (fs.FS, error) {
	b := fsys.getB(subsys, subname)
	b.lock.Lock()
	defer b.lock.Unlock()

	switch t := b.data.(type) {
	case notAnArchive:
		return nil, nil
	case fs.FS:
		return t, nil
	case fsysGenerator:
		fsys2, err := instantiate(t, fsys.rapool, subsys, subname)
		if err != nil { // should this be remembered as a permanent error?
			return nil, err
		}
		b.data = fsys2
		return fsys2, nil
	default: // not yet decided
		gen, err := fsys.probeArchive(subsys, subname)
		if err != nil {
			return nil, err
		}
		if gen == nil {
			b.data = notAnArchive{}
			return nil, nil
		} else {
			b.data = gen
			fsys2, err := instantiate(gen, fsys.rapool, subsys, subname)
			if err != nil { // should this be remembered as a permanent error?
				return nil, err
			}
			b.data = fsys2
			return fsys2, nil
		}
	}
}

func (fsys *FS) getB(subsys fs.FS, subname string) *b {
	fsys.bMu.RLock()
	x, ok := fsys.burrows[key{subsys, subname}]
	fsys.bMu.RUnlock()

	if ok {
		return x
	}

	fsys.bMu.Lock()
	x, ok = fsys.burrows[key{subsys, subname}]
	if !ok { // recheck because we relinquished the lock
		x = new(b)
		fsys.burrows[key{subsys, subname}] = x
	}
	fsys.bMu.Unlock()

	return x
}
