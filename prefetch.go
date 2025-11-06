package main

import (
	"io"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if !f.enable {
		return f.File.(io.ReaderAt).ReadAt(p, off)
	}
	return f.File.(io.ReaderAt).ReadAt(p, off)
}

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	t := time.Now()
	defer func() { slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String()) }()

	_, files := walk.FilesInDiskOrder(fsys.root)

	var wg sync.WaitGroup
	for range 1 {
		wg.Go(func() {
			for p := range files {
				o := path{fsys, fsys.root, internpath.New(p)}
				o.prefetch()
			}
		})
	}
	wg.Wait()
}

func (o path) prefetch() {
	isar, subfsys, _ := o.getArchive(true)
	if isar {
		waysort, files := walk.FilesInDiskOrder(subfsys.fsys)
		slog.Info("prefetchDir", "path", subfsys.String(), "sortorder", waysort)
		for name := range files {
			subfsys.ShallowJoin(name).prefetch()
		}
	}
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

func (f *cachingFile) stopCaching()                { f.File.(io.ReaderAt).ReadAt(nil, 0); f.enable = false }
func (f *cachingFile) withoutCaching() io.ReaderAt { return f.File.(io.ReaderAt) }
