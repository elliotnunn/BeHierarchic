package main

import (
	"archive/zip"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math/bits"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

var bigmu sync.RWMutex

const (
	select_iseof_data_from_pfcache_where_id_eq_x = iota
	select_size_from_scache_where_id_eq_x
	insert_or_ignore_into_pfcache_id_iseof_data_values_xxx
	insert_or_ignore_into_scache_id_size_values_xx
	nQuery
)

var queriesToCompile = [...]string{
	select_iseof_data_from_pfcache_where_id_eq_x:           `SELECT iseof, data FROM pfcache WHERE id = ?;`,
	select_size_from_scache_where_id_eq_x:                  `SELECT size FROM scache WHERE id = ?;`,
	insert_or_ignore_into_pfcache_id_iseof_data_values_xxx: `INSERT OR IGNORE INTO pfcache (id, iseof, data) VALUES (?, ?, ?);`,
	insert_or_ignore_into_scache_id_size_values_xx:         `INSERT OR IGNORE INTO scache (id, size) VALUES (?, ?);`,
}

func (fsys *FS) setupDB(dsn string) {
	if dsn == "" {
		return
	}

	var err error
	fsys.db, err = sql.Open("sqlite", dsn)
	if err != nil {
		slog.Error("sqlFail", "dsn", dsn, "err", err)
	}
	slog.Info("sqlOK", "dsn", dsn)
	// db errors after this point are worth a panic
	fsys.db.SetMaxOpenConns(1)

	_, err = fsys.db.Exec(`
	PRAGMA journal_mode = WAL;
	PRAGMA synchronous = OFF;
	PRAGMA mmap_size = 0x1000000000;
	PRAGMA page_size = 65536;
	CREATE TABLE IF NOT EXISTS pfcache (
		id BLOB PRIMARY KEY,
		iseof BOOL,
		data BLOB
	) WITHOUT ROWID;
	CREATE TABLE IF NOT EXISTS scache (
		id BLOB PRIMARY KEY,
		size INTEGER
	) WITHOUT ROWID;
	`)

	for i, query := range queriesToCompile {
		q, err := fsys.db.Prepare(query)
		if err != nil {
			panic(err.Error() + ": " + query)
		}
		fsys.dbq[i] = q
	}

	if err != nil {
		panic(err)
	}
}

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if !f.enable || f.path.container.db == nil {
		return f.File.(io.ReaderAt).ReadAt(p, off)
	}

	// have some fun here...
	id := appendint(dbkey(f.path), uint64(off))

	bigmu.RLock()
	row := f.path.container.dbq[select_iseof_data_from_pfcache_where_id_eq_x].QueryRow(id)
	var (
		iseof int
		data  []byte
	)
	err = row.Scan(&iseof, &data)
	bigmu.RUnlock()
	if errors.Is(err, sql.ErrNoRows) {
		// slog.Info("sqlMis", "offset", off, "size", len(p), "path", f.path, "id", hex.EncodeToString(id))
		return f.readAtThru(p, off)
	} else if err != nil {
		panic(err)
	}
	// slog.Info("sqlHit", "id", hex.EncodeToString(id))

	srcErr := error(nil)
	if iseof != 0 {
		srcErr = io.EOF
	}
	n, err = subRead(p, off, data, off, srcErr)
	if err != errIncompleteRead {
		return n, err
	}

	// not satisfied
	return f.readAtThru(p, off)
}

func (f *cachingFile) readAtThru(p []byte, off int64) (n int, err error) {
	n, err = f.File.(io.ReaderAt).ReadAt(p, off)
	if n == 0 || err != nil && err != io.EOF {
		return n, err // nothing worth caching
	}

	// slog.Error("dataRead", "offset", off, "size", len(p), "path", f.path)

	id := appendint(dbkey(f.path), uint64(off))

	bigmu.Lock()
	_, serr := f.path.container.dbq[insert_or_ignore_into_pfcache_id_iseof_data_values_xxx].Exec(
		id, err == io.EOF, p)
	bigmu.Unlock()
	if serr != nil {
		panic(serr)
	}

	return n, err
}

func (fsys *FS) Prefetch() {
	slog.Info("prefetchStart")
	t := time.Now()
	defer func() {
		slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String())
		if fsys.db != nil {
			slog.Info("vacuumStart")
			t := time.Now()
			fsys.db.Exec(`VACUUM;`)
			slog.Info("vacuumStop", "duration", time.Since(t).Truncate(time.Second).String())
		}
	}()

	path{fsys, fsys.root, internpath.New(".")}.prefetchThisFS(runtime.GOMAXPROCS(-1))
}

func (o path) prefetchThisFS(concurrency int) {
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
				if rawstat.Size() < 0 && o.container.db != nil {
					id := dbkey(o) // important not to deadlock here
					bigmu.RLock()
					sizerow := o.container.dbq[select_size_from_scache_where_id_eq_x].QueryRow(id)
					var size int64
					sqerr := sizerow.Scan(&size)
					bigmu.RUnlock()
					if sqerr == nil {
						o.container.rapool.ReaderAt(o).SetSize(size)
					}
				}

				timer := time.AfterFunc(time.Second, func() { slog.Info("takingLongTime", "path", o) })
				isar, fsys, err := o.getArchive(true)
				timer.Stop()
				if err != nil {
					slog.Error("getArchiveError", "err", err, "path", o)
				}
				if isar {
					fsys.prefetchThisFS(1)
				}

				// if the size is a prized hard-to-calculate quantity then save it
				if rawstat.Size() < 0 {
					if cookedstat, cookederr := o.cookedStat(); cookederr == nil { // if hard to calculate...
						realsize := cookedstat.Size() // do the calculation while the reader is well advanced
						if o.container.db != nil {
							id := dbkey(o) // important not to deadlock here
							bigmu.Lock()
							_, serr := o.container.dbq[insert_or_ignore_into_scache_id_size_values_xx].Exec(id, realsize)
							bigmu.Unlock()
							if serr != nil {
								panic(serr)
							}
						}
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
func (f *cachingFile) makePanic()                  { f.File = nil }
func (f *cachingFile) withoutCaching() io.ReaderAt { return f.File.(io.ReaderAt) }

func appendint(buf []byte, n uint64) []byte {
	nbytes := 8 - bits.LeadingZeros64(n)/8
	buf = append(buf, byte(nbytes))
	for shift := nbytes*8 - 8; shift >= 0; shift -= 8 {
		buf = append(buf, byte(n>>shift))
	}
	return buf
}

func readint(buf []byte) ([]byte, uint64, bool) {
	if len(buf) == 0 || buf[0] > 8 || len(buf) < int(buf[0])+1 {
		return buf, 0, false
	}
	n := uint64(0)
	for range buf[0] {
		n <<= 8
		n |= uint64(buf[1])
		buf = buf[1:]
	}
	buf = buf[1:]
	return buf, n, true
}

func dbkey(o path) []byte {
	o.container.rMu.RLock()
	warps := []path{o}
	for o.fsys != o.container.root {
		o = o.container.reverse[o.fsys]
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

func onekey(buf []byte, o path) []byte {
	bypath := func() []byte {
		buf = append(buf, 0xff) // never used in UTF-8
		buf = append(buf, o.name.String()...)
		buf = append(buf, 0xff)
		return buf
	}

	// temporary special case to get zip files going faster
	if zip, ok := o.fsys.(*zip.Reader); ok {
		want := o.name.String()
		for i, f := range zip.File {
			if f.Name == want {
				buf = append(buf, 0xfc)
				buf = appendint(buf, uint64(i))
				return buf
			}
		}
		return bypath()
	}

	if s, err := o.rawStat(); err == nil {
		if s, ok := s.(interface{ Order() int64 }); ok {
			buf = append(buf, 0xfd)
			buf = appendint(buf, uint64(s.Order()))
			return buf
		}

		if id, ok := fileID(s); ok {
			t := s.ModTime()
			buf = append(buf, 0xfe)
			buf = appendint(buf, id)
			buf = appendint(buf, uint64(t.Unix()))
			buf = appendint(buf, uint64(t.Nanosecond()))
			return buf
		}
	}

	return bypath()
}

var errIncompleteRead = errors.New("internal incompleteness error")

func subRead(p []byte, off int64, srcP []byte, srcOff int64, srcErr error) (int, error) {
	end, srcEnd := bufend(p, off), bufend(srcP, srcOff)
	if srcOff > off {
		// src:   []
		// dst: []
		return 0, errIncompleteRead
	} else if srcEnd <= off {
		// src: []
		// dst:   []
		if srcErr == nil {
			return 0, errIncompleteRead
		} else {
			return 0, srcErr
		}
	} else if srcEnd < end {
		// src: []
		// dst: [  ]
		n := copy(p, srcP[off-srcOff:])
		if srcErr == nil {
			return n, errIncompleteRead
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

func bufend(p []byte, off int64) int64 { return off + int64(len(p)) }
