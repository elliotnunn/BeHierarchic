// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/apm"
	"github.com/elliotnunn/BeHierarchic/internal/hfs"
	"github.com/elliotnunn/BeHierarchic/internal/reader2readerat"
	"github.com/elliotnunn/BeHierarchic/internal/resourcefork"
	"github.com/elliotnunn/BeHierarchic/internal/singlefilefs"
	"github.com/elliotnunn/BeHierarchic/internal/sit"
	"github.com/elliotnunn/BeHierarchic/internal/tarfs"
	"github.com/therootcompany/xz"
)

const Special = "◆"

type w struct {
	// sub-fsys, to path-within-that-fs, then to special-dir-suffix, then to another sub-fsys
	burrows map[fs.FS]map[string]map[string]fs.FS
	root    fs.FS
	lock    sync.Mutex
}

func Wrapper(fsys fs.FS) fs.FS {
	return &w{
		root:    fsys,
		burrows: map[fs.FS]map[string]map[string]fs.FS{fsys: {}},
	}
}

// [w.Open] returns [fs.File] objects that additionally satisfy this interface (except directories)
type File interface {
	fs.File
	io.Seeker
	io.ReaderAt
}

// Okay, here's the tricky thing...
// FS implements Open returns File implements Stat returns FileInfo
// FS implements Open returns File implements ReadDir returns DirEntry implements Info returns FileInfo

// We need to keep a positive list of files that correspond with a burrow
// Do we need to keep a list of files that are not? Nah, too costly.
func (w *w) Open(name string) (retf fs.File, reterr error) {
	name, suppressSpecialSiblings := checkAndDeleteComponent(name, ".nodeeper")

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
	// We need to intercept these to insert extra elements
	if !suppressSpecialSiblings && !strings.Contains(name, Special+"resources") { // resource forks don't contain zip files
		if rdf, mightBeDir := f.(fs.ReadDirFile); mightBeDir {
			if s, err := f.Stat(); err == nil && s.IsDir() {
				f = &dirWithExtraChildren{
					ReadDirFile: rdf,
					parentTree:  w,
					ownPath:     name,
				}
			}
		}
	}
	return f, nil
}

func (w *w) listSpecialSiblings(name string) ([]string, error) {
	fsys, subpath, err := w.resolve(name)
	if err != nil {
		return nil, err
	}

	w.lock.Lock()
	defer w.lock.Unlock()

	fsyspaths, ok := w.burrows[fsys]
	if !ok {
		panic("every mountpoint should be in the mountpoint map")
	}

	pathwarps, ok := fsyspaths[subpath]
	if !ok {
		// Ignore error in attempting to probe file
		pathwarps = exploreFile(fsys, subpath)
		fsyspaths[subpath] = pathwarps
		for _, fsysToAddToMap := range pathwarps {
			w.burrows[fsysToAddToMap] = make(map[string]map[string]fs.FS)
		}
	}

	ret := make([]string, 0, len(pathwarps))
	for nameSuffix := range pathwarps {
		ret = append(ret, path.Base(name)+Special+nameSuffix)
	}
	return ret, nil
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
			pathwarps = exploreFile(fsys, name[el.pathStart:el.pathEnd])
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

func exploreFile(fsys fs.FS, name string) map[string]fs.FS {
	if strings.HasPrefix(path.Base(name), "._") { // don't explore AppleDouble files
		return nil
	}

	specialSiblings := make(map[string]fs.FS)

	fsys2, suffix := makeFSFromArchive(fsys, name)
	if fsys2 != nil {
		specialSiblings[suffix] = fsys2
	}

	fsysr := makeFSFromResourceFork(fsys, name)
	if fsysr != nil {
		specialSiblings["resources"] = fsysr
	}

	if len(specialSiblings) == 0 {
		specialSiblings = nil
	}
	for s := range specialSiblings {
		specialSiblings[s] = &reader2readerat.FS{FS: specialSiblings[s]}
	}
	return specialSiblings
}

// What kind of FS to present for this file? (Sadly can be an expensive function)
// Will leak an open file
func makeFSFromArchive(fsys fs.FS, name string) (fsys2 fs.FS, suffix string) {
	baseFile, err := fsys.Open(name)
	if err != nil {
		return
	}
	defer func() {
		if err != nil || fsys2 == nil {
			fsys2 = nil // protect against nil-interface values
			suffix = ""
			baseFile.Close()
		}
	}()

	f, ok := baseFile.(File)
	if !ok {
		return
	}

	var header []byte
	matchAt := func(s string, offset int) bool {
		if len(header) < offset+len(s) && len(header) == cap(header) {
			target := (offset + len(s) + 63) &^ 63
			header = slices.Grow(header, target-len(header))
			n, _ := f.ReadAt(header[len(header):cap(header)], int64(len(header)))
			header = header[:len(header)+n]
		}
		return len(header) >= offset+len(s) && string(header[offset:][:len(s)]) == s
	}

	switch {
	case matchAt("\x1f\x8b", 0): // gzip
		suffix = "stream"
		fsys2 = &singlefilefs.FS{
			Name: changeSuffix(path.Base(name), ".gz .gzip .tgz=.tar"),
			Size: -1, // calculate by reading (sadly!)
			FileOpener: func() (io.Reader, error) {
				_, err := f.Seek(0, io.SeekStart)
				if err != nil {
					return nil, err
				}
				return gzip.NewReader(f)
			},
		}
	case matchAt("BZ", 0): // bzip2
		suffix = "stream"
		fsys2 = &singlefilefs.FS{
			Name: changeSuffix(path.Base(name), ".bz .bz2 .bzip2 .tbz=.tar .tb2=.tar"),
			Size: -1, // calculate by reading (sadly!)
			FileOpener: func() (io.Reader, error) {
				_, err := f.Seek(0, io.SeekStart)
				if err != nil {
					return nil, err
				}
				return bzip2.NewReader(f), nil
			},
		}
	case matchAt("\xfd7zXZ\x00", 0): // xz
		suffix = "stream"
		fsys2 = &singlefilefs.FS{
			Name: changeSuffix(path.Base(name), ".xz .txz=.tar"),
			Size: -1, // calculate by reading (sadly!)
			FileOpener: func() (io.Reader, error) {
				_, err := f.Seek(0, io.SeekStart)
				if err != nil {
					return nil, err
				}
				return xz.NewReader(f, xz.DefaultDictMax)
			},
		}
	case matchAt("ER", 0): // Apple Partition Map
		suffix = "partitions"
		fsys2, err = apm.New(f)
	case matchAt("PK", 0): // Zip file
		suffix = "archive"
		fsys2, err = zip.NewReader(f, statSize(f))
	case matchAt("rLau", 10) || matchAt("StuffIt (c)1997-", 0):
		suffix = "archive"
		fsys2, err = sit.New(f)
	case matchAt("ustar\x00\x30\x30", 257), matchAt("ustar\x20\x20\x00", 257), // posix tar
		strings.HasSuffix(name, ".tar"), !matchAt("\x00", 0) && matchAt("\x00", 99) && !matchAt("\x00", 100):
		suffix = "archive"
		fsys2, err = tarfs.New(f)
	case matchAt("BD", 1024):
		suffix = "fs"
		fsys2, err = hfs.New(f)
	}
	return
}

func makeFSFromResourceFork(fsys fs.FS, name string) (ret fs.FS) {
	baseFile, err := fsys.Open(path.Join(path.Dir(name), "._"+path.Base(name)))
	if err != nil {
		return nil
	}
	defer func() {
		if ret == nil {
			baseFile.Close()
		}
	}()

	f, ok := baseFile.(File)
	if !ok {
		return nil
	}

	s, err := f.Stat()
	if err != nil || s.Size() < 324 || s.Mode()&fs.ModeType != 0 { // smallest possible AppleDoubled resource fork
		return nil
	}
	return &resourcefork.FS{AppleDouble: f, ModTime: s.ModTime()}
}

func statSize(f fs.File) int64 {
	stat, err := f.Stat()
	if err != nil {
		return 0
	}
	return stat.Size()
}

func changeSuffix(s string, suffixes string) string {
	for _, rule := range strings.Split(suffixes, " ") {
		from, to, _ := strings.Cut(rule, "=")
		if strings.HasSuffix(s, from) && len(s) > len(from) {
			return s[:len(s)-len(from)] + to
		}
	}
	return s
}

func checkAndDeleteComponent(name string, special string) (string, bool) {
	foundSpecial := false
	l := strings.Split(name, "/")
	var l2 []string
	for _, s := range l {
		if s == ".nodeeper" {
			foundSpecial = true
		} else {
			l2 = append(l2, s)
		}
	}
	if foundSpecial {
		if len(l2) == 0 {
			return ".", true
		} else {
			return path.Join(l2...), true
		}
	} else {
		return name, false
	}
}
