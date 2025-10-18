// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io"
	"io/fs"
	"log/slog"
	"path"
	"runtime"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/spinner"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
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
		isar, subsubsys, err := fsys.getArchive(subsys, el, true)
		if err != nil {
			return nil, "", err
		} else if !isar {
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

func (fsys *FS) getArchive(subsys fs.FS, subname string, needFS bool) (bool, fs.FS, error) {
	if subsys == fsys.root { // Undercooked files, do not touch
		switch path.Ext(subname) {
		case ".crdownload", ".part":
			return false, nil, nil
		}
	}

	b := fsys.getB(subsys, subname)
	b.lock.Lock()
	defer b.lock.Unlock()

again:
	switch t := b.data.(type) {
	default: // not yet decided
		gen, err := fsys.probeArchive(subsys, subname)
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
		fsys2, err := instantiate(t, fsys.rapool, subsys, subname)
		if err != nil { // should this be remembered as a permanent error?
			return false, nil, err
		}
		b.data = fsys2
		goto again
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

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	fsys.prefetch(fsys.root, ".", runtime.NumCPU())
	slog.Info("prefetchStop")
}

func (fsys *FS) prefetch(subsys fs.FS, infoname string, concurrency int) {
	waysort, files := walk.FilesInDiskOrder(subsys)
	slog.Info("prefetchDir", "path", infoname, "sortorder", waysort)

	wg := new(sync.WaitGroup)
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			for name := range files {
				isar, subsubsys, _ := fsys.getArchive(subsys, name, true)
				if isar {
					subname := path.Join(infoname, name+Special)
					fsys.prefetch(subsubsys, subname, 1) // no sub concurrency, is that a good idea?
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
