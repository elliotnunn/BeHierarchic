package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"math/bits"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cockroachdb/pebble/v2"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

const (
	// meant to be eye-catching, and must never be <= 8 (see appendint)
	offsetByte  = 0xcc // appended to a dbkey ~ "offset follows, value is data"
	sizeByte    = 0x55 // appended to a dbkey ~ "value is a size"
	inodeByte   = 0xfe // precedes a onekey ~ "inode + namehash follows"
	unicodeByte = 0xff // flanks onekey, never part of valid UTF-8 ~ "filename"
	eofByte     = 0xee // appended twice to a dbvalue ~ EOF
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
		p, err := unmarshalBufErr(iter.Value())
		slog.Info("dbDump",
			"key", hex.EncodeToString(iter.Key()),
			"len", len(p),
			"data", hex.EncodeToString(p),
			"err", err)
	}
}

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if f.isCaching() {
		n, err = f.getCache(p, off)
		if err != errNotFound {
			atomic.AddInt64(&f.path.container.scoreGood, int64(n))
			return
		}
	}

	n, err = f.randomAccessFile.ReadAt(p, off)

	if f.isCaching() {
		atomic.AddInt64(&f.path.container.scoreBad, int64(n))
		f.setCache(p[:n], off, err)

		// if n > 0 {
		// 	p2 := make([]byte, len(p))
		// 	n2, err2 := f.getCache(p2, off)
		// 	if n2 != n || err2 != err || !bytes.Equal(p2[:n2], p[:n]) {
		// 		slog.Error("expected", "key", hex.EncodeToString(dbkey(f.path)), "path", f.path, "off", off, "len", len(p), "n", n, "err", err, "data", hex.EncodeToString(p[:n]))
		// 		slog.Error("got", "off", off, "len", len(p), "n", n2, "err", err2, "data", hex.EncodeToString(p2[:n2]))
		// 		f.path.container.dumpDB()
		// 		panic("dread mismatch")
		// 	}
		// }
	}

	return
}

func (f *cachingFile) getCache(p []byte, off int64) (n int, err error) {
	if f.path.container.db == nil {
		return 0, errNotFound
	}

	idPrefix := append(dbkey(f.path), offsetByte)
	id := appendint(idPrefix, off)

	iter, dberr := f.path.container.db.NewIter(&pebble.IterOptions{
		LowerBound: id,
	})
	if dberr != nil {
		panic(err)
	}
	defer iter.Close()

	if !iter.First() {
		return 0, errNotFound
	}

	xid := iter.Key()
	if !bytes.HasPrefix(xid, idPrefix) {
		return 0, errNotFound
	}

	dbbuf, dberr := iter.ValueAndErr()
	if dberr != nil {
		slog.Error("pebbleIteratorValueErr", "err", dberr)
		return 0, errNotFound
	}
	xp, xerr := unmarshalBufErr(dbbuf)

	xbufend, ok := read1int(xid[len(idPrefix):])
	if !ok {
		return 0, errNotFound
	}
	xoff := bufStart(xp, xbufend)

	return subRead(p, off, xp, xoff, xerr)
}

func (f *cachingFile) setCache(p []byte, off int64, err error) {
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

		dbbuf, dberr := iter.ValueAndErr()
		if dberr != nil {
			panic(dberr)
		}
		xp, xerr := unmarshalBufErr(dbbuf)
		xp = slices.Clone(xp) // a copy that we own

		xbufend, ok := read1int(xid[len(idPrefix):])
		if !ok {
			break // questionable whether this is actually a good idea
		}
		xoff := bufStart(xp, xbufend)

		if bufJoin(&p, &off, xp, xoff) {
			if xerr != nil {
				err = xerr
			}
			batch.Delete(xid, &pebble.WriteOptions{})
		} else {
			break
		}
	}
	batch.Set(appendint(idPrefix, bufEnd(p, off)),
		marshalBufErr(p, err),
		&pebble.WriteOptions{})
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
	stopTick := make(chan struct{})
	go func() {
		tick := time.Tick(time.Second * 5)
		var mem runtime.MemStats
		for {
			select {
			case <-tick:
				runtime.ReadMemStats(&mem)
				ram := mem.HeapInuse + mem.StackInuse
				disk := progress.Load()
				slog.Info("prefetchProgress",
					"archiveBytes", disk,
					"archiveBytesPerSec", disk/int64(time.Since(t)/time.Second),
					"heapBytes", mem.HeapInuse,
					"stackBytes", mem.StackInuse,
					"ramArchiveRatio", strconv.FormatFloat(float64(ram)/float64(disk), 'f', 4, 64),
				)
				log.Printf("pebbleStats\n%+v", fsys.db.Metrics())
			case <-stopTick:
				return
			}
		}
	}()

	defer func() {
		close(stopTick)
		if fsys.db != nil {
			fsys.db.Flush()
		}
		slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String())
		slog.Info("prefetchSummary", "cachedBytes", atomic.LoadInt64(&fsys.scoreGood), "uncachedBytes", atomic.LoadInt64(&fsys.scoreBad))
	}()

	path{fsys, fsys.root, internpath.New(".")}.prefetchThisFS(runtime.GOMAXPROCS(-1), &progress)
}

func (o path) prefetchThisFS(concurrency int, progress *atomic.Int64) {
	if o.name != internpath.New(".") {
		panic("this should be a filesystem!!")
	}

	waysort, files := walk.FilesInDiskOrder(o.fsys)
	slog.Info("prefetchDir", "path", o, "sortorder", waysort)

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for p := range files {
				o := o.ShallowJoin(p)

				rawstat, rawerr := o.rawStat()
				if rawerr != nil {
					continue
				}
				if !rawstat.Mode().IsRegular() {
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
		accum = onekey(accum, o)
	}
	return accum
}

// onekey appends bytes identifying one path-within-a-filesystem
// - must be unique among paths in that filesystem
// - must not start with byte(offsetByte), or it will clash with other data
// - should sort in an order somewhat corresponding to the expected tree walk order
// - should be as short as possible
func onekey(buf []byte, o path) []byte {
	bypath := func() []byte {
		buf = append(buf, unicodeByte) // never used in UTF-8
		buf = append(buf, o.name.String()...)
		buf = append(buf, unicodeByte)
		return buf
	}

	byinode := func(i uint64) []byte {
		buf = append(buf, inodeByte)
		buf = appendint(buf, int64(i))
		var hash xxhash.Digest // salt k with hashed name to avoid inode number collisions
		hash.WriteString(o.name.String())
		buf = appendint(buf, int64(hash.Sum64()&0xffffffff))
		return buf
	}

	if o.fsys == o.container.root {
		o.container.iMu.RLock()
		ino, ok := o.container.ino[o.name]
		o.container.iMu.RUnlock()
		if ok {
			return byinode(ino)
		}
	}

	if s, err := o.rawStat(); err == nil {
		if s, ok := s.(interface{ Order() int64 }); ok {
			buf = appendint(buf, s.Order())
			return buf
		}

		if o.fsys == o.container.root {
			if ino, ok := inodeNum(s); ok {
				o.container.iMu.Lock()
				if o.container.ino == nil {
					o.container.ino = make(map[internpath.Path]uint64)
				}
				o.container.ino[o.name] = ino
				o.container.iMu.Unlock()
				return byinode(ino)
			}
		}
	}

	return bypath()
}

var errNotFound = errors.New("internal incompleteness error")

func subRead(p []byte, off int64, srcP []byte, srcOff int64, srcErr error) (int, error) {
	end, srcEnd := bufEnd(p, off), bufEnd(srcP, srcOff)
	if srcOff > off {
		// src:   []
		// dst: []
		return 0, errNotFound
	} else if srcEnd <= off {
		// src: []
		// dst:   []
		if srcErr == nil {
			return 0, errNotFound
		} else {
			return 0, srcErr
		}
	} else if srcEnd < end {
		// src: []
		// dst: [  ]
		n := copy(p, srcP[off-srcOff:])
		if srcErr == nil {
			return n, errNotFound
		} else {
			return n, srcErr
		}
	} else {
		// src: [  ]
		// dst: []
		n := copy(p, srcP[off-srcOff:])
		if srcEnd == end {
			return n, srcErr
		} else {
			return n, nil
		}
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

// marshalBufErr makes the buffer end with {eofByte, eofByte} IFF it is EOF
func marshalBufErr(p []byte, err error) []byte {
	p = p[:len(p):len(p)] // append could be catastrophic
	if len(p) > 0 && p[len(p)-1] == eofByte {
		// less desirable case: need to lengthen by 2 bytes
		if err == io.EOF {
			return append(p, eofByte, eofByte)
		} else {
			return append(p, 0, eofByte)
		}
	} else {
		if err == io.EOF {
			return append(p, eofByte, eofByte)
		} else {
			return p // common case
		}
	}
}

// unmarshalBufErr trickily undoes the work of marshalBufErr
func unmarshalBufErr(buf []byte) (p []byte, err error) {
	if len(buf) >= 2 && buf[len(buf)-1] == eofByte {
		if buf[len(buf)-2] == eofByte {
			return buf[:len(buf)-2], io.EOF
		} else {
			return buf[:len(buf)-2], nil
		}
	}
	return buf, nil
}
