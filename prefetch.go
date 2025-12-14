package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"math/bits"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/cockroachdb/pebble/v2"
	"github.com/elliotnunn/BeHierarchic/internal/fileid"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

const (
	// meant to be eye-catching, and must never be <= 8 (see appendint)
	offsetByte = 0xcc // appended to a dbkey ~ "offset follows, value is data"
	sizeByte   = 0x55 // appended to a dbkey ~ "value is a size"
)

func (fsys *FS) setupDB(dsn string) {
	if dsn == "" {
		return
	}

	db, err := pebble.Open(dsn, &pebble.Options{})
	if err != nil {
		slog.Error("dbFail", "path", dsn, "err", err)
		return
	}
	slog.Info("dbOK", "dsn", dsn)
	fsys.db = db
}

func (fsys *FS) dumpDB() {
	iter, err := fsys.db.NewIter(&pebble.IterOptions{})
	if err != nil {
		panic(err)
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		slog.Info("dbDump",
			"key", hex.EncodeToString(iter.Key()),
			"len", len(iter.Value()),
			"data", hex.EncodeToString(iter.Value()))
	}
}

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if f.isCaching() {
		n = f.getCache(p, off)
		atomic.AddInt64(&f.path.container.scoreGood, int64(n))
		if n == len(p) {
			return n, nil
		}
	}

	more, err := f.randomAccessFile.ReadAt(p[n:], off+int64(n))
	n += more

	if f.isCaching() && more > 0 {
		atomic.AddInt64(&f.path.container.scoreBad, int64(n))
		f.setCache(p[:n], off)

		// if n > 0 {
		// 	p2 := make([]byte, len(p))
		// 	n2 := f.getCache(p2, off)
		// 	if n2 != n || !bytes.Equal(p2[:n2], p[:n]) {
		// 		slog.Error("cacheMismatch",
		// 			"key", hex.EncodeToString(dbkey(f.path)),
		// 			"path", f.path,
		// 			"off", off,
		// 			"len", len(p))
		// 		slog.Error("cacheMismatchExpect",
		// 			"n", n,
		// 			"data", hex.EncodeToString(p[:n]))
		// 		slog.Error("cacheMismatchGot",
		// 			"n", n2,
		// 			"data", hex.EncodeToString(p2[:n2]))
		// 		f.path.container.dumpDB()
		// 	}
		// }
	}

	return
}

func (f *cachingFile) getCache(p []byte, off int64) (n int) {
	if f.path.container.db == nil {
		return 0
	}

	idPrefix := append(dbkey(f.path), offsetByte)
	id := appendint(idPrefix, off)

	iter, dberr := f.path.container.db.NewIter(&pebble.IterOptions{
		LowerBound: id,
	})
	if dberr != nil {
		panic(dberr)
	}
	defer iter.Close()

	if !iter.First() {
		return 0
	}

	xid := iter.Key()
	if !bytes.HasPrefix(xid, idPrefix) {
		return 0
	}

	xp, dberr := iter.ValueAndErr()
	if dberr != nil {
		slog.Error("pebbleIteratorValueErr", "err", dberr)
		return 0
	}

	xbufend, ok := read1int(xid[len(idPrefix):])
	if !ok {
		return 0
	}
	xoff := bufStart(xp, xbufend)

	return subRead(p, off, xp, xoff)
}

func (f *cachingFile) setCache(p []byte, off int64) {
	if f.path.container.db == nil || len(p) == 0 {
		return
	}

	// do not accidentally append over someone else's data!
	p = p[:len(p):len(p)]

	idPrefix := append(dbkey(f.path), offsetByte)
	idPrefix = idPrefix[:len(idPrefix):len(idPrefix)] // clashing append
	id := appendint(idPrefix, off)

	batch := f.path.container.db.NewBatch()

	iter, dberr := f.path.container.db.NewIter(&pebble.IterOptions{
		LowerBound: id,
	})
	if dberr != nil {
		panic(dberr)
	}

	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() { // be *super* careful not to munge values from the db
		xid := iter.Key()
		if !bytes.HasPrefix(xid, idPrefix) {
			break
		}
		xid = slices.Clone(xid)

		xp, dberr := iter.ValueAndErr()
		if dberr != nil {
			panic(dberr)
		}
		xp = slices.Clone(xp) // a copy that we own

		xbufend, ok := read1int(xid[len(idPrefix):])
		if !ok {
			break // questionable whether this is actually a good idea
		}
		xoff := bufStart(xp, xbufend)

		if bufJoin(&p, &off, xp, xoff) {
			batch.Delete(xid, &pebble.WriteOptions{})
		} else {
			break
		}
	}
	batch.Set(appendint(idPrefix, bufEnd(p, off)), p, &pebble.WriteOptions{})
	dberr = batch.Commit(&pebble.WriteOptions{})
	if dberr != nil {
		panic(dberr)
	}
}

func (o path) getCacheSize() (int64, bool) {
	if o.container.db == nil {
		return 0, false
	}
	id := append(dbkey(o), sizeByte)
	val, closer, err := o.container.db.Get(id)
	if err == pebble.ErrNotFound {
		return 0, false
	} else if err != nil {
		slog.Error("getCacheSizeError", "path", o, "err", err)
		return 0, false
	}
	defer closer.Close()
	return read1int(val)
}

func (o path) setCacheSize(s int64) {
	if o.container.db == nil {
		return
	}
	id := append(dbkey(o), sizeByte)
	val := appendint([]byte(nil), s)
	err := o.container.db.Set(id, val, &pebble.WriteOptions{})
	if err != nil {
		slog.Error("setCacheSizeError", "path", o, "err", err)
	}
}

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	atomic.StoreInt64(&fsys.scoreGood, 0)
	atomic.StoreInt64(&fsys.scoreBad, 0)

	t := time.Now()
	var progress atomic.Int64
	printProgress := func() {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		ram := mem.HeapInuse + mem.StackInuse
		disk := progress.Load()
		fsys.rMu.RLock()
		mounts := len(fsys.reverse)
		fsys.rMu.RUnlock()
		slog.Info("prefetchProgress",
			"t", time.Since(t).Truncate(time.Second).String(),
			"mounts", thouSep(int64(mounts)),
			"archiveBytes", thouSep(disk),
			"ramPerArchive", strconv.FormatFloat(float64(ram)/float64(disk), 'f', 3, 64),
			"cacheHitBytes", thouSep(atomic.LoadInt64(&fsys.scoreGood)),
			"cacheMissBytes", thouSep(atomic.LoadInt64(&fsys.scoreBad)),
		)
	}

	stopTick := make(chan struct{})
	go func() {
		tick := time.Tick(time.Second * 5)
		for {
			select {
			case <-tick:
				printProgress()
			case <-stopTick:
				return
			}
		}
	}()

	// the time consuming part
	path{fsys, fsys.root, internpath.New(".")}.prefetchThisFS(runtime.GOMAXPROCS(-1), &progress)

	close(stopTick)
	if fsys.db != nil {
		fsys.db.Flush()
	}
	printProgress()
	slog.Info("prefetchStop")
}

type selfWalking interface {
	Walk(waitFull bool) iter.Seq2[string, fs.FileMode]
}

func (o path) prefetchThisFS(concurrency int, progress *atomic.Int64) {
	if o.name != internpath.New(".") {
		panic("this should be a filesystem!!")
	}

	slog.Info("prefetchDir", "path", o)

	ch := make(chan internpath.Path)
	go func() {
		defer close(ch)
		// prepare a list of files using a method that depends on the kind of filesystem
		if selfWalking, ok := o.fsys.(selfWalking); ok {
			for pathname, kind := range selfWalking.Walk(true /*exhaustive*/) {
				if kind.IsRegular() {
					ch <- internpath.New(pathname)
				}
			}
		} else {
			var list []internpath.Path
			fs.WalkDir(o.fsys, ".", func(pathname string, d fs.DirEntry, err error) error {
				if d.Type().IsRegular() {
					list = append(list, internpath.New(pathname))
				}
				return nil
			})
			slices.SortStableFunc(list, func(a, b internpath.Path) int {
				ao, bo := o, o
				ao.name, bo.name = a, b
				apos, bpos := ao.identify(), bo.identify()
				return bytes.Compare(apos[:], bpos[:])
			})
			for _, p := range list {
				ch <- p
			}
		}
	}()

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for name := range ch {
				o := o
				o.name = name

				rawstat, rawerr := o.rawStat()
				if rawerr != nil {
					continue
				}

				// if we have a valuable size then use it
				sizeInCache := false
				if rawstat.Size() < 0 && o.container.db != nil {
					size, ok := o.getCacheSize()
					if ok {
						o.container.rapool.ReaderAt(o).SetSize(size)
						sizeInCache = true
					}
				}
				if progress != nil {
					progress.Add(rawstat.Size())
				}

				timer := time.AfterFunc(time.Second, func() { slog.Info("takingLongTime", "path", o) })
				isar, fsys := o.getArchive(true)
				timer.Stop()
				if isar && !strings.HasPrefix(o.name.Base(), "._") { // no use probing resource forks!
					fsys.prefetchThisFS(1, nil)
				}

				// if the size is a prized hard-to-calculate quantity then save it
				// opportune to do the calc now while the reader would be well advanced into the file
				if easysize := rawstat.Size(); easysize < 0 && !sizeInCache {
					realsize, ok := o.container.rapool.ReaderAt(o).SizeIfPossible()
					if ok {
						o.setCacheSize(realsize)
					}
				}
			}
		})
	}
	wg.Wait()
}

// please don't use on a directory!
func (o path) prefetchCachedOpen() (*cachingFile, error) {
	f, err := o.cookedOpen()
	if err != nil {
		return nil, err
	}
	rdr, ok := f.(randomAccessFile)
	if !ok { // ???not a file
		return nil, fs.ErrInvalid
	}
	return &cachingFile{path: o, randomAccessFile: rdr}, nil
}

type randomAccessFile interface {
	io.ReaderAt
	Stat() (fs.FileInfo, error)
	io.Closer
}

type cachingFile struct {
	path path
	randomAccessFile
}

func (f *cachingFile) isCaching() bool                  { return f.path.container != nil }
func (f *cachingFile) stopCaching()                     { f.path = path{} }
func (f *cachingFile) makePanic()                       { f.randomAccessFile = nil }
func (f *cachingFile) withoutCaching() randomAccessFile { return f.randomAccessFile }

func appendint(buf []byte, n int64) []byte {
	u := uint64(n)
	nbytes := 8 - bits.LeadingZeros64(u)/8
	buf = append(buf, byte(nbytes))
	for shift := nbytes*8 - 8; shift >= 0; shift -= 8 {
		buf = append(buf, byte(u>>shift))
	}
	return buf
}

func read1int(buf []byte) (int64, bool) {
	if len(buf) == 0 || buf[0] > 8 || len(buf) != int(buf[0])+1 {
		return 0, false
	}
	buf = buf[1:]
	n := int64(0)
	for len(buf) != 0 {
		n <<= 8
		n |= int64(buf[0])
		buf = buf[1:]
	}
	return n, true
}

func (o path) identify() (ret fileid.ID) {
	var id fileid.ID

	if fsk, ok := o.fsys.(*fskeleton.FS); ok {
		stat, err := fsk.Lstat(o.name.String())
		if err == nil {
			idnum := uint64(stat.(fskeleton.FileInfo).ID())
			binary.BigEndian.PutUint64(id[len(id)-8:], idnum)
			return id
		}
	}

	if o.fsys == o.container.root {
		o.container.iMu.RLock()
		var ok bool
		id, ok = o.container.idCache[o.name]
		o.container.iMu.RUnlock()
		if ok {
			return id
		}
		id, _ = fileid.Get(o.fsys, o.name.String())
	}

	// Fall back on hashing the filename
	if id == *new(fileid.ID) {
		var h xxhash.Digest
		// could optimise by adding a WriteTo method to internpath.Path
		h.WriteString(o.name.String())
		binary.BigEndian.PutUint64(id[len(id)-8:], h.Sum64())
	}

	if o.fsys == o.container.root {
		o.container.iMu.Lock()
		o.container.idCache[o.name] = id
		o.container.iMu.Unlock()
	}

	return id
}

// dbkey creates a key for a file using a series of [onekey] calls
// - remember to append offsetByte or sizeByte
// - durable across appends
// - inherits the sort-order properties from onekey
// - likely to have capacity to append -- be careful of clashing appends
func dbkey(o path) []byte {
	o.container.rMu.RLock()
	warps := []path{o}
	for o.fsys != o.container.root {
		o = o.container.reverse[o.fsys].Thick(o.container)
		warps = append(warps, o)
	}
	o.container.rMu.RUnlock()

	slices.Reverse(warps)

	var accum []byte
	for _, o := range warps {
		id := o.identify()
		slice := id[:]
		for len(slice) > 0 && slice[0] == 0 {
			slice = slice[1:]
		}
		accum = append(accum, byte(len(slice)))
		accum = append(accum, slice...)
	}
	return accum
}

func subRead(p []byte, off int64, srcP []byte, srcOff int64) (n int) {
	if srcOff > off {
		// src:   [
		// dst: [
		return 0
	} else if bufEnd(srcP, srcOff) <= off {
		// src: ]
		// dst:   [
		return 0
	} else {
		return copy(p, srcP[off-srcOff:])
	}
}

func bufJoin(p1 *[]byte, off1 *int64, p2 []byte, off2 int64) bool {
	end1, end2 := bufEnd(*p1, *off1), bufEnd(p2, off2)
	if end1 < off2 || end2 < *off1 {
		return false // no overlap
	}
	if off2 < *off1 { // swap so leftmost is 1
		*off1, off2 = off2, *off1
		end1, end2 = end2, end1
		*p1, p2 = p2, *p1
	}
	if end2 > end1 {
		*p1 = append(*p1, p2[end1-off2:]...)
	}
	return true
}

func bufEnd(p []byte, off int64) int64   { return off + int64(len(p)) }
func bufStart(p []byte, end int64) int64 { return end - int64(len(p)) }

func thouSep(n int64) string {
	var s []byte
	s = strconv.AppendInt(s, n, 10)
	nsep := (len(bytes.TrimLeft(s, "-")) - 1) / 3
	s = append(s, make([]byte, nsep)...)
	for i, from, to := 0, len(s)-nsep-3, len(s)-3; i < nsep; i, from, to = i+1, from-3, to-4 {
		copy(s[to:][:3], s[from:])
		s[to-1] = '_'
	}
	return unsafe.String(&s[0], len(s))
}
