// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io"
	"io/fs"
	"log/slog"
	gopath "path"
	"runtime"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/spinner"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

const Special = "◆"

type FS struct {
	bMu     sync.RWMutex
	burrows map[path]*b

	rMu     sync.RWMutex
	reverse map[fs.FS]path

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
type fsysGenerator func(io.ReaderAt) (fs.FS, error)

func Wrapper(fsys fs.FS) *FS {
	const blockShift = 13 // 8 kb
	return &FS{
		root:    fsys,
		burrows: make(map[path]*b),
		reverse: make(map[fs.FS]path),
		rapool:  spinner.New(blockShift, memLimit>>blockShift, 200 /*open readers at once*/),
	}
}

func (fsys *FS) getArchive(o path, needFS bool) (bool, fs.FS, error) {
	if o.fsys == fsys.root { // Undercooked files, do not touch
		switch gopath.Ext(o.name.Base()) {
		case ".crdownload", ".part":
			return false, nil, nil
		}
	}

	b := fsys.getB(o)
	b.lock.Lock()
	defer b.lock.Unlock()

again:
	switch t := b.data.(type) {
	default: // not yet decided
		gen, err := fsys.probeArchive(o)
		if err != nil {
			return false, nil, err
		}
		if gen == nil {
			b.data = notAnArchive{}
		} else {
			b.data = gen
		}
		goto again
	case notAnArchive:
		return false, nil, nil
	case fs.FS:
		return true, t, nil
	case fsysGenerator:
		if !needFS {
			return true, nil, nil
		}

		f, err := o.Open()
		if err != nil {
			return false, nil, err
		}
		r, nativeReaderAt := f.(io.ReaderAt)
		if !nativeReaderAt {
			f.Close()
			f = nil
			r = fsys.rapool.ReaderAt(o)
		}
		fsys2, err := t(r)
		if err != nil {
			if f != nil {
				f.Close()
			}
			return false, nil, err
		}

		fsys.rMu.Lock()
		fsys.reverse[fsys2] = o
		fsys.rMu.Unlock()
		b.data = fsys2
		goto again
	}
}

func (fsys *FS) getB(o path) *b {
	fsys.bMu.RLock()
	x, ok := fsys.burrows[o]
	fsys.bMu.RUnlock()

	if ok {
		return x
	}

	fsys.bMu.Lock()
	x, ok = fsys.burrows[o]
	if !ok { // recheck because we relinquished the lock
		x = new(b)
		fsys.burrows[o] = x
	}
	fsys.bMu.Unlock()

	return x
}

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	t := time.Now()
	o, _ := fsys.path(".")
	fsys.prefetch(o, runtime.NumCPU())
	slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String())
}

func (fsys *FS) prefetch(o path, concurrency int) {
	waysort, files := walk.FilesInDiskOrder(o.fsys)
	slog.Info("prefetchDir", "path", o.String(), "sortorder", waysort)

	wg := new(sync.WaitGroup)
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			for name := range files {
				isar, _, _ := fsys.getArchive(o.Join(name), true)
				if isar {
					p, _ := fsys.path(o.Join(name).String() + Special) // we were promised this exists
					fsys.prefetch(p, 1)                                // no sub concurrency, is that a good idea?
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
