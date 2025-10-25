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

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
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

func (o path) getArchive(needFS bool) (bool, fs.FS, error) {
	if o.fsys == o.container.root { // Undercooked files, do not touch
		switch gopath.Ext(o.name.Base()) {
		case ".crdownload", ".part":
			return false, nil, nil
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
			r = o.container.rapool.ReaderAt(o)
		}
		fsys2, err := t(r)
		if err != nil {
			if f != nil {
				f.Close()
			}
			return false, nil, err
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

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	t := time.Now()
	o, _ := fsys.path(".")
	o.prefetch(runtime.NumCPU())
	slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String())
}

func (o path) prefetch(concurrency int) {
	waysort, files := walk.FilesInDiskOrder(o.fsys)
	slog.Info("prefetchDir", "path", o.String(), "sortorder", waysort)

	wg := new(sync.WaitGroup)
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			for name := range files {
				isar, subfsys, _ := o.ShallowJoin(name).getArchive(true)
				if isar {
					path{o.container, subfsys, internpath.New(".")}.prefetch(1)
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
