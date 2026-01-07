// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"errors"
	"io/fs"
	"log/slog"
	gopath "path"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"github.com/elliotnunn/BeHierarchic/internal/fileid"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

const Special = "◆"

type FS struct {
	mMu    sync.RWMutex
	mounts map[thinPath]*mount // nonexistent or nil or pointer

	rMu     sync.RWMutex
	reverse map[fs.FS]thinPath

	db *pebble.DB

	iMu     sync.RWMutex
	idCache map[internpath.Path]fileid.ID

	scoreGood, scoreBad int64

	root fs.FS
}

// if not present in the map, the file has not yet been scanned
// if nil pointer, the file has been scanned and is not an archive (common)
// if non-nil pointer, meaning depends on data as below...
type mount struct {
	lock sync.Mutex
	data any
	// nil          = not sure yet (temporary state)
	// func()       = archive creator-function
	// fs.FS        = FS
}

type fsysGenerator func() (fs.FS, error)

func fsysGeneratorNop() (fs.FS, error) { return nil, nil }

func Wrapper(fsys fs.FS, cachePath string) *FS {
	const blockShift = 12 // 4 kb -- must match the AppleDouble resourcefork padding!

	fsys2 := &FS{
		root:    fsys,
		mounts:  make(map[thinPath]*mount),
		reverse: make(map[fs.FS]thinPath),
		idCache: make(map[internpath.Path]fileid.ID),
	}
	fsys2.setupDB(cachePath)
	return fsys2
}

func (o path) getArchive(needKnow, needFS bool) (bool, path) {
	// Do not probe resources in a resource fork: expensive and unproductive
	if o.isInResourceForkFS() {
		return false, path{}
	}

	if o.fsys == o.container.root { // Undercooked files, do not touch
		switch gopath.Ext(o.name.Base()) {
		case ".crdownload", ".part":
			return false, path{}
		}
	}

	// Low-RAM path for non-archive paths inside an fskeleton FS
	if fskel, ok := o.fsys.(*fskeleton.FS); ok {
		if bozo, ok := fskel.GetBozo(o.name); ok {
			if bozo == 1 {
				return false, path{}
			}
		}
	}

	if !needKnow {
		o.container.mMu.RLock()
		b, ok := o.container.mounts[o.Thin()]
		o.container.mMu.RUnlock()
		if !ok || b == nil {
			return false, path{}
		}
		b.lock.Lock()
		fsys, ok := b.data.(fs.FS)
		b.lock.Unlock()
		if !ok {
			return false, path{}
		}
		return true, path{o.container, fsys, internpath.Path{}}
	}

	locksets := [...]struct{ lock, unlock func() }{
		{o.container.mMu.RLock, o.container.mMu.RUnlock},
		{o.container.mMu.Lock, o.container.mMu.Unlock},
	}

	var b *mount

lockloop:
	for i, mu := range locksets {
		mu.lock()
		var ok bool
		b, ok = o.container.mounts[o.Thin()]
		switch {
		case !ok && i == 0:
			// Unknown file, but we lack write access, so try again
			mu.unlock()
			continue
		case !ok && i == 1:
			// Unknown file, create a blank mount struct
			b = new(mount)
			o.container.mounts[o.Thin()] = b
			mu.unlock()
			break lockloop
		case ok && b == nil:
			// Known NOT to be a mount
			mu.unlock()
			return false, path{}
		case ok && b != nil:
			// Either a suspected mount, or a certain mount
			mu.unlock()
			break lockloop
		}
	}

	b.lock.Lock()
	defer b.lock.Unlock()

again:
	switch t := b.data.(type) {
	default: // not yet decided
		gen, err := o.probeArchive()
		if errors.Is(err, fs.ErrNotExist) {
			o.container.mMu.Lock()
			delete(o.container.mounts, o.Thin())
			o.container.mMu.Unlock()
			goto notEvenAFile
		} else if err != nil {
			slog.Warn("archiveProbeError", "path", o, "err", err)
		}
		if err != nil || gen == nil {
			goto notAnArchive
		}
		b.data = gen
		goto again
	case fs.FS:
		return true, path{o.container, t, internpath.Path{}}
	case fsysGenerator:
		if !needFS {
			return true, path{}
		}

		fsys2, err := t()
		if err != nil {
			slog.Warn("archiveInstantiateError", "path", o, "err", err)
		}
		if err != nil || fsys2 == nil {
			goto notAnArchive
		}

		o.container.rMu.Lock()
		o.container.reverse[fsys2] = o.Thin()
		o.container.rMu.Unlock()
		b.data = fsys2
		goto again
	}

notAnArchive:
	if fskel, ok := o.fsys.(*fskeleton.FS); ok {
		fskel.SetBozo(o.name, 1)
		o.container.mMu.Lock()
		delete(o.container.mounts, o.Thin()) // here is the RAM saving!
		o.container.mMu.Unlock()
	} else {
		o.container.mMu.Lock()
		o.container.mounts[o.Thin()] = nil
		o.container.mMu.Unlock()
	}
notEvenAFile:
	// small chance that another instance of this function raced to get this mount structure
	b.data = fsysGeneratorNop
	return false, path{}
}

func (o path) isInResourceForkFS() bool {
	// this code is optimised to avoid an allocation from internpath.Path.Base
	o.container.rMu.RLock()
	tainer := o.container.reverse[o.fsys].name
	o.container.rMu.RUnlock()
	var tbuf [128]byte
	tainer.PutBase(tbuf[:])
	return string(tbuf[:2]) == "._"
}
