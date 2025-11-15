// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"database/sql"
	"io/fs"
	gopath "path"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/spinner"
	_ "modernc.org/sqlite"
)

const Special = "◆"

type FS struct {
	bMu     sync.RWMutex
	burrows map[path]*b

	rMu     sync.RWMutex
	reverse map[fs.FS]path

	dbMu sync.RWMutex
	db   *sql.DB
	dbq  [nQuery]*sql.Stmt

	zMu     sync.RWMutex
	zipLocs map[path]int64

	root   fs.FS
	rapool *spinner.Pool
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
type fsysGenerator func() (fs.FS, error)

func Wrapper(fsys fs.FS, cachePath string) *FS {
	const blockShift = 13 // 8 kb

	fsys2 := &FS{
		root:    fsys,
		burrows: make(map[path]*b),
		reverse: make(map[fs.FS]path),
		rapool:  spinner.New(blockShift, memLimit>>blockShift, 200 /*open readers at once*/),
	}
	fsys2.setupDB(cachePath)
	return fsys2
}

func (o path) getArchive(needFS bool) (bool, path, error) {
	if strings.HasPrefix(o.name.Base(), "._") {
		return false, path{}, nil // AppleDouble files don't currently contain an archive
	}
	if o.fsys == o.container.root { // Undercooked files, do not touch
		switch gopath.Ext(o.name.Base()) {
		case ".crdownload", ".part":
			return false, path{}, nil
		}
	}

	b := o.getB()
	b.lock.Lock()
	defer b.lock.Unlock()

again:
	switch t := b.data.(type) {
	default: // not yet decided
		gen, err := o.probeArchive()
		if err != nil {
			return false, path{}, err
		}
		if gen == nil {
			b.data = notAnArchive{}
		} else {
			b.data = gen
		}
		goto again
	case notAnArchive:
		return false, path{}, nil
	case fs.FS:
		return true, path{o.container, t, internpath.New(".")}, nil
	case fsysGenerator:
		if !needFS {
			return true, path{}, nil
		}

		fsys2, err := t()
		if err != nil {
			return false, path{}, err
		}

		o.container.rMu.Lock()
		o.container.reverse[fsys2] = o
		o.container.rMu.Unlock()
		b.data = fsys2
		goto again
	}
}

func (o path) getB() *b {
	o.container.bMu.RLock()
	x, ok := o.container.burrows[o]
	o.container.bMu.RUnlock()

	if ok {
		return x
	}

	o.container.bMu.Lock()
	x, ok = o.container.burrows[o]
	if !ok { // recheck because we relinquished the lock
		x = new(b)
		o.container.burrows[o] = x
	}
	o.container.bMu.Unlock()

	return x
}
