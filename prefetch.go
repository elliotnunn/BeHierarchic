package main

import (
	"archive/zip"
	"bytes"
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
	select_le = iota
	select_gt
	select_size_from_scache_where_id_eq_x
	pfcache_insert
	pfcache_delete
	insert_or_ignore_into_scache_id_size_values_xx
	nQuery
)

var queriesToCompile = [...]string{
	select_le:                             `SELECT id, iseof, data FROM pfcache WHERE id <= ? ORDER BY id DESC LIMIT 1;`,
	select_gt:                             `SELECT id, iseof, data FROM pfcache WHERE id > ? ORDER BY id ASC;`,
	select_size_from_scache_where_id_eq_x: `SELECT size FROM scache WHERE id = ?;`,
	pfcache_insert:                        `INSERT OR IGNORE INTO pfcache (id, iseof, data) VALUES (?, ?, ?);`,
	pfcache_delete:                        `DELETE FROM pfcache WHERE id = ?;`,
	insert_or_ignore_into_scache_id_size_values_xx: `INSERT OR IGNORE INTO scache (id, size) VALUES (?, ?);`,
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
	docache := f.enable && f.path.container.db != nil

	if docache {
		n, err = f.getCache(p, off)
		if err != errNotFound {
			// fmt.Printf("%s: ReadAt(%d,%d) = (%v,%s)\n",
			// 	f.path, len(p), off, err, hex.EncodeToString(p[:n]))
			return
		}
	}

	n, err = f.File.(io.ReaderAt).ReadAt(p, off)

	if docache {
		f.setCache(p[:n], off, err)

		// if n == len(p) {
		// 	cp := make([]byte, len(p))
		// 	_, cerr := f.getCache(cp, off)
		// 	if !bytes.Equal(cp, p) {
		// 		panic(fmt.Sprintf("%s: ReadAt(%d,%d) = (%v,%s) but got (%v,%s)\n",
		// 			f.path, len(p), off, err, hex.EncodeToString(p[:n]),
		// 			cerr, hex.EncodeToString(cp[:n])))
		// 	}
		// }
	}

	return
}

func (f *cachingFile) getCache(p []byte, off int64) (n int, err error) {
	idPrefix := dbkey(f.path)
	id := appendint(idPrefix, off)

	bigmu.RLock()
	defer bigmu.RUnlock()

	var (
		xid    []byte
		xiseof bool
		xp     []byte
	)
	err = f.path.container.dbq[select_le].QueryRow(id).Scan(&xid, &xiseof, &xp)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotFound
	} else if err != nil {
		panic(err)
	}

	if !bytes.HasPrefix(xid, idPrefix) {
		return 0, errNotFound
	}
	xoff, ok := read1int(xid[len(idPrefix):])
	if !ok {
		return 0, errNotFound
	}

	return subRead(p, off, xp, xoff, iseof2err(xiseof))
}

func (f *cachingFile) setCache(p []byte, off int64, err error) {
	if len(p) == 0 || err != nil && err != io.EOF {
		return // no point caching
	}

	// do not accidentally append over someone else's data!
	p = p[:len(p):len(p)]
	iseof := err == io.EOF

	// slight subtlety about which rows we want
	idPrefix := dbkey(f.path)
	idPrefix = idPrefix[:len(idPrefix):len(idPrefix)] // clashing append
	idMax := appendint(idPrefix, bufEnd(p, off))
	id := appendint(idPrefix, off)
	var delrows [][]byte

	bigmu.Lock()
	defer bigmu.Unlock()

	// fmt.Printf("searching %s-%s (%d bytes)\n", hex.EncodeToString(idPrefix), hex.EncodeToString(id[len(idPrefix):]), len(p))

	for _, q := range []int{select_le, select_gt} {
		rows, err := f.path.container.dbq[q].Query(id)
		if err != nil {
			panic(err)
		}
		for rows.Next() {
			var (
				xid    []byte
				xiseof bool
				xp     []byte
			)
			serr := rows.Scan(&xid, &xiseof, &xp)
			if serr == sql.ErrNoRows {
				break
			} else if serr != nil {
				panic(serr)
			}

			// fmt.Printf("nearby row %s (%d bytes)\n", hex.EncodeToString(xid), len(xp))
			if !bytes.HasPrefix(xid, idPrefix) || bytes.Compare(xid, idMax) > 0 {
				// fmt.Println("-- toofar")
				break // gone too far
			}
			xoff, ok := read1int(xid[len(idPrefix):])
			if !ok {
				// fmt.Println("-- nan")
				continue
			}

			if bufJoin(&p, &off, xp, xoff) {
				iseof = iseof || xiseof
				id = appendint(idPrefix, off)
				delrows = append(delrows, xid)
				// fmt.Printf("-- consuming: now %s-%s (%d bytes)\n", hex.EncodeToString(idPrefix), hex.EncodeToString(id[len(idPrefix):]), len(p))
			}
		}
		rows.Close()
	}

	for _, delid := range delrows {
		f.path.container.dbq[pfcache_delete].Exec(delid)
	}
	// fmt.Println("---------------")
	f.path.container.dbq[pfcache_insert].Exec(id, iseof, p)
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
	n := int64(0)
	for range buf[0] {
		n <<= 8
		n |= int64(buf[1])
		buf = buf[1:]
	}
	return n, true
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
				o, err := f.DataOffset()
				if err != nil {
					buf = append(buf, 0xfc)
					buf = appendint(buf, int64(i))
				} else { // happier path
					buf = appendint(buf, o)
				}
				return buf
			}
		}
		return bypath()
	}

	if s, err := o.rawStat(); err == nil {
		if s, ok := s.(interface{ Order() int64 }); ok {
			buf = appendint(buf, s.Order())
			return buf
		}

		if id, ok := fileID(s); ok {
			t := s.ModTime()
			buf = append(buf, 0xfe)
			buf = appendint(buf, int64(id))
			buf = appendint(buf, t.Unix())
			buf = appendint(buf, int64(t.Nanosecond()))
			return buf
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

func bufEnd(p []byte, off int64) int64 { return off + int64(len(p)) }
func iseof2err(iseof bool) error {
	if iseof {
		return io.EOF
	} else {
		return nil
	}
}
