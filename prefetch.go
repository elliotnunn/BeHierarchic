package main

import (
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
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
					path{o.container, subfsys, internpath.New(".")}.prefetch(1)
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

