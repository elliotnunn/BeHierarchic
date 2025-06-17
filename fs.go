package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/apm"
	"github.com/elliotnunn/BeHierarchic/internal/hfs"
	"github.com/elliotnunn/BeHierarchic/internal/reader2readerat"
	"github.com/elliotnunn/BeHierarchic/internal/sit"
)

const Special = "◆"

type w struct {
	root    fs.FS
	burrows map[fs.FS]map[string]map[string]fs.FS // actually I don't care much for the string, but it's important for debuggability
	lock    sync.Mutex
}

func Wrapper(fsys fs.FS) fs.FS {
	return &w{
		root:    fsys,
		burrows: map[fs.FS]map[string]map[string]fs.FS{fsys: {}},
	}
}

// Okay, here's the tricky thing...
// FS implements Open returns File implements Stat returns FileInfo
// FS implements Open returns File implements ReadDir returns DirEntry implements Info returns FileInfo

// We need to keep a positive list of files that correspond with a burrow
// Do we need to keep a list of files that are not? Nah, too costly.
func (w *w) Open(name string) (retf fs.File, reterr error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	fsys, subdir, err := w.resolve(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}

	f, err := fsys.Open(subdir)
	if err != nil {
		return nil, err // would be nice to make this more informative
	}

	// The returned object might be a directory and receive ReadDir calls.
	// We need these ReadDir calls to know when a child file is a disk image,
	// and make it look like a directory.
	if rdf, mightBeDir := f.(fs.ReadDirFile); mightBeDir {
		f = &dirWithExtraChildren{
			ReadDirFile: rdf,
			extraChildren: func(realChildren []fs.DirEntry) (fakeChildren []fs.DirEntry) {
				w.lock.Lock()
				defer w.lock.Unlock()
				for _, c := range realChildren {
					fsyspaths, ok := w.burrows[fsys]
					if !ok {
						panic("every mountpoint should be in the mountpoint map")
					}

					childName := path.Join(subdir, c.Name())
					pathwarps, ok := fsyspaths[childName]
					if !ok {
						pathwarps, err = exploreFile(fsys, childName, path.Join(name, c.Name()))
						if err != nil {
							continue // likely FNF, tidy this return up later
						}
						fsyspaths[childName] = pathwarps
						for _, fsysToAddToMap := range pathwarps {
							w.burrows[fsysToAddToMap] = make(map[string]map[string]fs.FS)
						}
					}

					for kind := range pathwarps {
						fakeChildren = append(fakeChildren, &dirEntry{
							name: c.Name() + Special + kind,
						})
					}
				}
				return fakeChildren
			},
		}
	}
	return f, nil
}

func (w *w) resolve(name string) (fsys fs.FS, subpath string, err error) {
	type element struct {
		pathStart, pathEnd int // something like "a/b/c"
		diveStart, diveEnd int // there is a "Special" between path and subpath
	}
	var warps []element
	i := 0
	for i < len(name) {
		pathLen := strings.Index(name[i:], Special)
		if pathLen == -1 {
			break
		}
		diveLen := strings.IndexByte(name[i+pathLen+len(Special):], '/')
		if diveLen == -1 { // last path element
			warps = append(warps, element{
				i, i + pathLen, // start/end of path part
				i + pathLen + len(Special), len(name)}) // start/end of warp part
			i = len(name)
		} else {
			warps = append(warps, element{
				i, i + pathLen, // start/end of path part
				i + pathLen + len(Special), i + pathLen + len(Special) + diveLen}) // start/end of warp part
			i += pathLen + len(Special) + diveLen + 1
		}
	}
	tail := name[i:]
	if tail == "" {
		tail = "."
	}

	fsys = w.root
	w.lock.Lock()
	defer w.lock.Unlock()
	for _, el := range warps {
		fsyspaths, ok := w.burrows[fsys]
		if !ok {
			panic("every mountpoint should be in the mountpoint map")
		}

		pathwarps, ok := fsyspaths[name[el.pathStart:el.pathEnd]]
		if !ok {
			pathwarps, err = exploreFile(fsys,
				name[el.pathStart:el.pathEnd], // for fsys.Open()
				name[:el.pathEnd])             // unique cache key for reader2readerat
			if err != nil {
				return nil, "", err // likely FNF, tidy this return up later
			}
			fsyspaths[name[el.pathStart:el.pathEnd]] = pathwarps
			for _, fsysToAddToMap := range pathwarps {
				w.burrows[fsysToAddToMap] = make(map[string]map[string]fs.FS)
			}
		}

		fsys, ok = pathwarps[name[el.diveStart:el.diveEnd]]
		if !ok {
			return nil, "", fs.ErrNotExist
		}
	}
	return fsys, tail, nil
}

func exploreFile(fsys fs.FS, name string, uniq string) (map[string]fs.FS, error) {
	// Open data fork, could it possibly be an archive?
	o, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	s, err := o.Stat()
	if err != nil {
		return nil, err
	}
	if s.IsDir() {
		return nil, errors.New("not a directory")
	}

	subdir, kind := exploreDataFork(o.(io.ReaderAt))
	if subdir == nil { // nope, it's just a file
		o.Close()
		return nil, nil
	}
	subdir = &reader2readerat.FS{FS: subdir, Uniq: uniq}

	// TODO: resource forks
	return map[string]fs.FS{kind: subdir}, nil
}

func exploreDataFork(file io.ReaderAt) (fs.FS, string) {
	var magic [16]byte
	if n, _ := file.ReadAt(magic[:], 0); n < 16 {
		return nil, ""
	}
	switch {
	case string(magic[:2]) == "ER": // Apple Partition Map
		fsys, err := apm.New(file)
		if err == nil {
			return fsys, "partitions"
		}
	case string(magic[:2]) == "PK": // Zip file (kinda, it's complicated)
		fsys, err := zip.NewReader(file, size(file))
		if err == nil {
			return fsys, "files"
		}
	case string(magic[10:14]) == "rLau" || string(magic[:16]) == "StuffIt (c)1997-":
		fsys, err := sit.New(file)
		if err == nil {
			return fsys, "files"
		}
	}

	if n, _ := file.ReadAt(magic[:2], 1024); n < 2 {
		return nil, ""
	}
	if string(magic[:2]) == "BD" {
		view, err := hfs.New(file)
		if err == nil {
			return view, "files"
		}
	}
	return nil, ""
}

func size(f io.ReaderAt) int64 {
	type sizer interface {
		Size() int64
	}
	switch as := f.(type) {
	case sizer:
		return as.Size()
	case fs.File:
		stat, err := as.Stat()
		if err != nil {
			panic("failed to stat an open file")
		}
		return stat.Size()
	case io.Seeker:
		prev, err := as.Seek(0, io.SeekCurrent)
		if err != nil {
			panic("failed to seek to current seek location")
		}
		size, err := as.Seek(0, io.SeekEnd)
		if err != nil {
			panic("failed to seek to end")
		}
		_, err = as.Seek(prev, io.SeekStart)
		if err != nil {
			panic("failed to undo our seeking")
		}
		return size
	default: // binary-search for the size
		var lbound, ubound int64
		var buf [1]byte
		for i := int64(0); ; i = max(i, 1) * 2 {
			n, _ := f.ReadAt(buf[:], i)
			if n == 1 {
				lbound = i + 1
			} else {
				ubound = i
				break
			}
		}
		for lbound != ubound {
			mid := lbound + (ubound-lbound)/2
			n, _ := f.ReadAt(buf[:], mid)
			if n == 1 {
				lbound = mid + 1
			} else {
				ubound = mid
			}
		}
		return lbound
	}
}
