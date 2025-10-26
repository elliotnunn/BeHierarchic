package main

import (
	"io"
	"io/fs"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

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
					subfsys.prefetch(1)
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

// TODO: use these nice functions to speed up the preflight stage
func (o path) preflightCachedRead(p []byte, off int64) (n int, err error) {
	return -1, nil
}
func (o path) postflightCachedRead(p []byte, off int64, err error) {
}

// please don't use on a directory!
func (o path) prefetchCachedOpen() (*cachingFile, error) {
	f, err := o.cookedOpen()
	if err != nil {
		return nil, err
	}
	_, ok := f.(io.ReaderAt)
	if !ok { // ???not a file
		return nil, fs.ErrInvalid
	}
	return &cachingFile{path: o, File: f, enable: true}, nil
}

type cachingFile struct {
	path path
	fs.File
	enable bool
}

func (f *cachingFile) stopCaching()                { f.enable = false }
func (f *cachingFile) withoutCaching() io.ReaderAt { return f.File.(io.ReaderAt) }

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if f.enable {
		n, err = f.path.preflightCachedRead(p, off)
		if n >= 0 {
			return n, err
		}
	}
	n, err = f.File.(io.ReaderAt).ReadAt(p, off)
	if f.enable {
		f.path.postflightCachedRead(p[:n], off, err)
	}
	return n, err
}
