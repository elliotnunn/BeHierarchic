package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/walk"
)

var bigmu sync.Mutex

const (
	doSomeStuffQuery = iota
	nQuery
)

func (fsys *FS) setupDB(dsn string) {
	var err error
	fsys.db, err = sql.Open("sqlite", dsn)
	if err != nil {
		slog.Error("sqlFail", "dsn", dsn, "err", err)
	}
	slog.Info("sqlOK", "dsn", dsn)
	// db errors after this point are worth a panic
	fsys.db.SetMaxOpenConns(1)

	_, err = fsys.db.Exec(`CREATE TABLE IF NOT EXISTS pfcache (
		id BLOB PRIMARY KEY,
		iseof BOOL,
		data BLOB
	) WITHOUT ROWID;
	`)

	if err != nil {
		panic(err)
	}
}

func (f *cachingFile) ReadAt(p []byte, off int64) (n int, err error) {
	if !f.enable {
		return f.File.(io.ReaderAt).ReadAt(p, off)
	}

	// have some fun here...
	id := fmt.Sprintf("%s//%012x", f.path, off)

	bigmu.Lock()
	row := f.path.container.db.QueryRow("SELECT iseof, data FROM pfcache WHERE id = ?", id)
	var (
		iseof int
		data  []byte
	)
	err = row.Scan(&iseof, &data)
	bigmu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		// slog.Info("sqlMis", "id", id)
		return f.readAtThru(p, off)
	} else if err != nil {
		panic(err)
	}
	// slog.Info("sqlHit", "id", id)

	if len(data) >= len(p) || iseof != 0 { // full satisfaction
		n = copy(p, data)
		if iseof != 0 && len(data) <= len(p) {
			err = io.EOF
		}
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

	id := fmt.Sprintf("%s//%012x", f.path, off)

	bigmu.Lock()
	_, serr := f.path.container.db.Exec(
		`INSERT OR REPLACE INTO pfcache (id, iseof, data) VALUES (?, ?, ?);`,
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
	defer func() { slog.Info("prefetchStop", "duration", time.Since(t).Truncate(time.Second).String()) }()

	_, files := walk.FilesInDiskOrder(fsys.root)

	var wg sync.WaitGroup
	for range runtime.NumCPU() {
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

func dbkey(o path, offset int64) string {
	
	// return o.stableString() + "\x00"
}
