// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io/fs"
	"log/slog"
	gopath "path"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/spinner"
)

const Special = "◆"

type FS struct {
	mMu    sync.RWMutex
	mounts map[path]*mount // nonexistent or nil or pointer

	rMu     sync.RWMutex
	reverse map[fs.FS]path

	db *pebble.DB

	iMu sync.RWMutex
	ino map[internpath.Path]uint64

	scoreGood, scoreBad int64

	root   fs.FS
	rapool *spinner.Pool
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
		mounts:  make(map[path]*mount),
		reverse: make(map[fs.FS]path),
		rapool:  spinner.New(blockShift, memLimit>>blockShift, 200 /*open readers at once*/),
	}
	fsys2.setupDB(cachePath)
	return fsys2
}

func (o path) getArchive(needFS bool) (bool, path) {
	// Do not probe resources in a resource fork: expensive and unproductive
	o.container.rMu.RLock()
	inResourceFork := strings.HasPrefix(o.container.reverse[o.fsys].name.Base(), "._")
	o.container.rMu.RUnlock()
	if inResourceFork {
		return false, path{}
	}

	if o.fsys == o.container.root { // Undercooked files, do not touch
		switch gopath.Ext(o.name.Base()) {
		case ".crdownload", ".part":
			return false, path{}
		}
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
		b, ok = o.container.mounts[o]
		mu.unlock()
		switch {
		case !ok && i == 0:
			// Unknown file, but we lack write access, so try again
			continue
		case !ok && i == 1:
			// Unknown file, create a blank mount struct
			b = new(mount)
			break lockloop
		case ok && b == nil:
			// Known NOT to be a mount
			mu.unlock()
			return false, path{}
		case ok && b != nil:
			// Either a suspected mount, or a certain mount
			b = b
			break lockloop
		}
	}

	b.lock.Lock()
	defer b.lock.Unlock()

again:
	switch t := b.data.(type) {
	default: // not yet decided
		gen, err := o.probeArchive()
		if err != nil {
			slog.Warn("archiveProbeError", "path", o, "err", err)
		}
		if err != nil || gen == nil {
			goto notAnArchive
		}
		b.data = gen
		goto again
	case fs.FS:
		return true, path{o.container, t, internpath.New(".")}
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
		o.container.reverse[fsys2] = o
		o.container.rMu.Unlock()
		b.data = fsys2
		goto again
	}

notAnArchive:
	o.container.mMu.Lock()
	o.container.mounts[o] = nil
	o.container.mMu.Unlock()
	// small chance that another instance of this function raced to get this mount structure
	b.data = fsysGeneratorNop
	return false, path{}
}
