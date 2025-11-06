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
	Size    map[cacheHash]int64 // 0 = unknown, ^size = known
	Extents map[cacheHash]byteRangeList
	dirty   bool
}

type cacheHash [2]uint64

func (o path) rootPair() (path, cacheHash) {
	// Get a hash of the path relative to the outermost archive file
	// (saved to disk -- don't change!)
	pivot := o
	var hash cacheHash // zero hash means root
	if pivot.fsys != pivot.container.root {
		h := xxh3.New()
		for pivot.fsys != pivot.container.root {
			h.WriteString(pivot.name.String())
			h.WriteString("//")
			pivot.container.rMu.RLock()
			pivot = pivot.container.reverse[pivot.fsys]
			pivot.container.rMu.RUnlock()
		}
		hs := h.Sum128()
		hash[0] = hs.Hi
		hash[1] = hs.Lo
	}
	return pivot, hash
}

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if !f.enable {
		return f.File.(io.ReaderAt).ReadAt(p, off)
	}
	prf := &f.path.container.prf

	pivot, hash := f.path.rootPair()

	// Tricky file-length stuff
	smallp := p
	prf.cMu.Lock()
	size := prf.cache[pivot].Size[hash]
	if size >= 0 {
		if off >= size {
			err = io.EOF
			smallp = nil
		} else if off+int64(len(p)) > size {
			err = io.EOF
			smallp = p[:size-off]
		}
	}
	ok := prf.cache[pivot].Extents[hash].Get(smallp, off)
	prf.cMu.Unlock()
	if ok || len(smallp) == 0 {
		return len(smallp), err // yay a cache hit!
	}

	n, err = f.File.(io.ReaderAt).ReadAt(p, off)

	// PUT IT IN THE CACHE
	if n > 0 {
		prf.cMu.Lock()
		defer prf.cMu.Unlock()
		if prf.cache[pivot] == nil {
			return n, err // not actually interested in this one
		}
		prf.cache[pivot].Extents[hash] = prf.cache[pivot].Extents[hash].Set(p, off)
		prf.cache[pivot].dirty = true
	}
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

func readCacheFile(name string) cacheFile {
	dflt := cacheFile{
		Size:    make(map[cacheHash]int64),
		Extents: make(map[cacheHash]byteRangeList),
		dirty:   true, // so that it will be written out
	}

	f, err := os.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return dflt
	} else if err != nil {
		slog.Error("cacheReadError", "err", err)
		return dflt
	}
	defer f.Close()

	var cache cacheFile
	err = gob.NewDecoder(f).Decode(&cache)
	if err != nil {
		slog.Error("cacheReadError", "err", err)
		return dflt
	}
	return cache
}

func writeCacheFile(name string, cache cacheFile) {
	for h, s := range cache.Size {
		if s == 0 {
			delete(cache.Size, h)
		}
	}
	tmp := filepath.Join(filepath.Dir(name), "~"+filepath.Base(name))
	err := os.MkdirAll(filepath.Dir(tmp), 0o777)
	if err != nil {
		slog.Warn("errorWhileSavingCache", "err", err)
		return
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
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
	err = os.Rename(tmp, name)
	if err != nil {
		slog.Warn("errorWhileRenamingCache", "err", err)
		return
	}
}

func (o path) prefetchRealFile() {
	cpath := cacheFilePath(o.container.prf.path, o)
	if cpath == "" { // caching disabled
		o.prefetch()
		return
	}

	cache := readCacheFile(cpath)

	o.container.prf.cMu.Lock()
	o.container.prf.cache[o] = &cache
	o.container.prf.cMu.Unlock()

	o.prefetch() // take long time

	o.container.prf.cMu.Lock()
	delete(o.container.prf.cache, o)
	o.container.prf.cMu.Unlock()

	if cache.dirty {
		writeCacheFile(cpath, cache)
	}
}

func (o path) prefetch() {
	pivot, hash := o.rootPair()

	// Reconcile the size information in the archive header and the cache
	// (either or both may be absent)
	// Remembering that the size field in the cache is inverted for efficiency
	easySize := int64(-1)
	if s, err := o.rawStat(); err == nil {
		easySize = s.Size()
	}

	o.container.prf.cMu.Lock()
	sizeInCache := ^o.container.prf.cache[pivot].Size[hash]
	o.container.prf.cMu.Unlock()

	switch {
	case easySize >= 0 && sizeInCache >= 0:
		// happy, nothing we need to do
	case easySize < 0 && sizeInCache < 0:
		// sad, nothing we can do
	case easySize < 0 && sizeInCache >= 0:
		// use the cached value
		o.container.rapool.ReaderAt(o).SetSize(sizeInCache)
	case easySize > 0 && sizeInCache < 0:
		// use the header value
		o.container.prf.cMu.Lock()
		o.container.prf.cache[pivot].Size[hash] = ^easySize
		o.container.prf.cMu.Unlock()
	}

	isar, subfsys, _ := o.getArchive(true)
	if isar {
		waysort, files := walk.FilesInDiskOrder(subfsys.fsys)
		slog.Info("prefetchDir", "path", subfsys.String(), "sortorder", waysort)
		for name := range files {
			subfsys.ShallowJoin(name).prefetch()
		}
	}

	// This has the added welcome effect of ensuring that the actual size gets calced
	s, err := o.cookedStat()
	if err != nil {
		slog.Error("prefetchStatFail", "err", err, "path", o.String())
		return // we truly don't know the size
	}
	finalSize := s.Size()

	slog.Info("prefetchSizes", "sizeInCache", sizeInCache, "easySize", easySize, "finalSize", finalSize, "path", o)

	// Now leave the size in the cache iff it is hard to calculate
	if easySize >= 0 {
		finalSize = -1
	}
	o.container.prf.cMu.Lock()
	o.container.prf.cache[pivot].Size[hash] = ^finalSize
	if finalSize != sizeInCache {
		o.container.prf.cache[pivot].dirty = true
	}
	o.container.prf.cMu.Unlock()
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
