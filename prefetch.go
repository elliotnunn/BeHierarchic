package main

import (
	"encoding/gob"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
	"github.com/zeebo/xxh3"
)

type cacheFile struct { // will be gobbed, so take care with the format
	Version string
	Cache   map[cacheHash]byteRangeList
}

type cacheHash [2]uint64

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if !f.enable {
		return f.File.(io.ReaderAt).ReadAt(p, off)
	}
	prf := &f.path.container.prf

	pivot := f.path
	var hash cacheHash // zero hash means root
	if pivot.fsys != pivot.container.root {
		h := xxh3.New()
		for pivot.fsys != pivot.container.root {
			h.WriteString(pivot.name.Base())
			h.WriteString("//")
			pivot.container.rMu.RLock()
			pivot = pivot.container.reverse[pivot.fsys]
			pivot.container.rMu.RUnlock()
		}
		hs := h.Sum128()
		hash[0] = hs.Hi
		hash[1] = hs.Lo
	}

	prf.cMu.Lock()
	if c, ok := prf.cache[pivot]; ok {
		if c, ok := c.Cache[hash]; ok {
			if c.Get(p, off) {
				prf.cMu.Unlock()
				return len(p), nil // yay a cache hit!
			}
		}
	}
	prf.cMu.Unlock()

	n, err = f.File.(io.ReaderAt).ReadAt(p, off)
	if n < len(p) {
		return n, err
	}

	// PUT IT IN THE CACHE
	prf.cMu.Lock()
	defer prf.cMu.Unlock()
	if prf.cache[pivot] == nil {
		return n, err // not actually interested in this one
	}
	if prf.cache[pivot].Cache[hash] == nil {

	}
	brl := prf.cache[pivot].Cache[hash]
	brl.Set(p, off)
	prf.cache[pivot].Cache[hash] = brl
	return n, err
}

func initPrefetcher(cachePath string) prefetcher {
	return prefetcher{
		path:  cachePath,
		cache: make(map[path]*cacheFile),
	}
}

type prefetcher struct {
	path string

	cMu   sync.Mutex
	cache map[path]*cacheFile // zero hash is the "file itself"
}

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	t := time.Now()
	defer func() { slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String()) }()

	_, files := walk.FilesInDiskOrder(fsys.root)

	var wg sync.WaitGroup
	for range 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range files {
				o := path{fsys, fsys.root, internpath.New(p)}
				o.prefetchRealFile()
			}
		}()
	}
	wg.Wait()
}

func (o path) prefetchRealFile() {
	slog := slog.With("archive", o.String())

	cpath := cacheFilePath(o.container.prf.path, o)
	if cpath == "" { // caching disabled
		isar, subfsys, _ := o.getArchive(true)
		if isar {
			subfsys.prefetch(1)
		}
		return
	}

	cache := cacheFile{Version: "BeHierarchic-1", Cache: make(map[cacheHash]byteRangeList)}

	f, err := os.Open(cpath)
	if errors.Is(err, fs.ErrNotExist) {
		goto freshCache
	} else if err != nil {
		slog.Error("cacheReadError", "err", err)
		goto freshCache
	}

	err = gob.NewDecoder(f).Decode(&cache)
	f.Close()
	if err != nil {
		slog.Error("cacheReadError", "err", err)
		goto freshCache
	}

freshCache:
	o.container.prf.cMu.Lock()
	o.container.prf.cache[o] = &cache
	o.container.prf.cMu.Unlock()

	// Do the hard work, might take a good while
	isar, subfsys, _ := o.getArchive(true)
	if isar {
		subfsys.prefetch(1)
	}
	// this might take a while, and once it's done, the cache will be populated

	o.container.prf.cMu.Lock()
	delete(o.container.prf.cache, o)
	o.container.prf.cMu.Unlock()

	// Here decide whether it is a cache worth having...

	// let's write it unconditionally for now
	tmp := filepath.Join(filepath.Dir(cpath), "~"+filepath.Base(cpath))
	err = os.MkdirAll(filepath.Dir(tmp), 0o777)
	if err != nil {
		slog.Warn("errorWhileSavingCache", "err", err)
		return
	}
	f, err = os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		slog.Warn("errorWhileSavingCache", "err", err)
		return
	}
	defer f.Close()
	err = gob.NewEncoder(f).Encode(cache)
	if err != nil {
		slog.Warn("errorWhileGobbingCache", "err", err)
		return
	}
	f.Close()
	err = os.Rename(tmp, cpath)
	if err != nil {
		slog.Warn("errorWhileRenamingCache", "err", err)
		return
	}
	slog.Info("successfulCache")
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

func cacheFilePath(base string, child path) string {
	const ext = ".becache"
	if base == "" {
		return ""
	}
	p, err := filepath.Localize(child.String() + ext)
	if err != nil { // bad file name on Windows
		return ""
	}
	return filepath.Join(base, p)
}

type cachingFile struct {
	path path
	fs.File
	enable bool
}

func (f *cachingFile) stopCaching()                { f.File.(io.ReaderAt).ReadAt(nil, 0); f.enable = false }
func (f *cachingFile) withoutCaching() io.ReaderAt { return f.File.(io.ReaderAt) }
