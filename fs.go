// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/spinner"
)

const Special = "◆"

type w struct {
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

func Wrapper(fsys fs.FS) fs.FS {
	const blockShift = 13 // 8 kb
	return &w{
		root:    fsys,
		burrows: make(map[key]*b),
		rapool:  spinner.New(blockShift, memLimit>>blockShift, 200 /*open readers at once*/),
	}
}

func (w *w) resolve(name string) (fsys fs.FS, subpath string, err error) {
	warps := strings.Split(name, Special+"/")
	if strings.HasSuffix(name, Special) {
		warps[len(warps)-1] = strings.TrimSuffix(warps[len(warps)-1], Special)
		warps = append(warps, ".")
	}
	warps, name = warps[:len(warps)-1], warps[len(warps)-1]

	fsys = w.root
	for _, el := range warps {
		fsys2, err := w.whatArchive(fsys, el)
		if err != nil {
			return nil, "", err
		} else if fsys2 == nil {
			return nil, "", fs.ErrNotExist
		}
		fsys = fsys2
	}
	return fsys, name, nil
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

func (w *w) isArchive(fsys fs.FS, name string) (bool, error) {
	b := w.getB(fsys, name)
	b.lock.Lock()
	defer b.lock.Unlock()

	switch b.data.(type) {
	case notAnArchive:
		return false, nil
	case fsysGenerator, fs.FS:
		return true, nil
	default: // not yet decided
		gen, err := w.probeArchive(fsys, name)
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

func (w *w) whatArchive(fsys fs.FS, name string) (fs.FS, error) {
	b := w.getB(fsys, name)
	b.lock.Lock()
	defer b.lock.Unlock()

	switch t := b.data.(type) {
	case notAnArchive:
		return nil, nil
	case fs.FS:
		return t, nil
	case fsysGenerator:
		fsys2, err := instantiate(t, w.rapool, fsys, name)
		if err != nil { // should this be remembered as a permanent error?
			return nil, err
		}
		b.data = fsys2
		return fsys2, nil
	default: // not yet decided
		gen, err := w.probeArchive(fsys, name)
		if err != nil {
			return nil, err
		}
		if gen == nil {
			b.data = notAnArchive{}
			return nil, nil
		} else {
			b.data = gen
			fsys2, err := instantiate(gen, w.rapool, fsys, name)
			if err != nil { // should this be remembered as a permanent error?
				return nil, err
			}
			b.data = fsys2
			return fsys2, nil
		}
	}
}

func (w *w) getB(fsys fs.FS, name string) *b {
	w.bMu.RLock()
	x, ok := w.burrows[key{fsys, name}]
	w.bMu.RUnlock()

	if ok {
		return x
	}

	w.bMu.Lock()
	x, ok = w.burrows[key{fsys, name}]
	if !ok { // recheck because we relinquished the lock
		x = new(b)
		w.burrows[key{fsys, name}] = x
	}
	w.bMu.Unlock()

	return x
}
