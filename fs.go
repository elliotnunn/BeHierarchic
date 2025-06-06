package main

import (
	"archive/zip"
	"io"
	"io/fs"
	"path"

	"github.com/elliotnunn/resourceform/internal/apm"
	"github.com/elliotnunn/resourceform/internal/hfs"
	"github.com/elliotnunn/resourceform/internal/reader2readerat"
	"github.com/elliotnunn/resourceform/internal/sit"
)

type w struct {
	burrows map[string]fs.FS
}

func Wrapper(fsys fs.FS) fs.FS {
	return &w{
		burrows: map[string]fs.FS{
			".": fsys,
		},
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

	pathcut, err := w.resolve(name)
	if err != nil {
		return nil, err
	}

	left, right := pcut(name, pathcut)
	o, err := w.burrows[left].Open(right)
	if err != nil {
		return nil, err // rare case, because we already successfully statted
	}

	// The returned object might be a directory and receive ReadDir calls.
	// We need these ReadDir calls to know when a child file is a disk image,
	// and make it look like a directory.
	if _, mightBeDir := o.(fs.ReadDirFile); mightBeDir {
		o = fileWithReadDirFilter{
			ReadDirFile: o.(fs.ReadDirFile),
			filter: func(e *fs.DirEntry) {
				if (*e).IsDir() {
					return
				}
				path := path.Join(name, (*e).Name())
				pathlen := plen(path)
				pathcut, _ := w.resolve(path)
				if pathcut < pathlen {
					return
				}
				stat, err := fs.Stat(w, path)
				if err != nil {
					return
				}
				*e = mountPointEntry{
					diskImageStat: stat,
				}
			},
		}
	}

	// If the returned File object is the root of a disk image,
	// then its Stat should return info about the file EXCEPT pretend to be a directory
	if pathcut > 0 && pathcut == plen(name) {
		parentcut, err := w.resolve(pleft(name, pathcut-1))
		if err != nil {
			panic("inconsistency in the burrow list")
		}
		left, right := pcut(name, parentcut)
		stat, err := fs.Stat(w.burrows[left], right)
		if err != nil {
			panic("unable to stat a file that certainly exists")
		}

		o = fileWithFileInfoOverride{
			ReadDirFile: o.(fs.ReadDirFile),
			stat: mountPointEntry{
				diskImageStat: stat,
			},
		}
	}
	return o, nil
}

// We could use some seriously better caching
// And are we particularly wedded to the idea of statting the file first?
// (It's probably a boondoggle, could unfortunately lead to replication)
func (w *w) resolve(name string) (int, error) {
	// This is the fairly happy path
	pathlen := plen(name)
	var fsys, fsysNew fs.FS
	var pathcut int
	var err error

	for pathcut = pathlen; pathcut >= 0; pathcut-- {
		fsys = w.burrows[pleft(name, pathcut)]
		if fsys != nil {
			break
		}
	}
	if fsys == nil {
		panic("fsys should have been found")
	}

tryAgainWithANewlyDiscoveredImage:
	fsysNew, err = try1(fsys, pright(name, pathcut), name)
	if err == nil {
		if fsysNew == nil {
			return pathcut, nil
		} else {
			w.burrows[name] = fsysNew
			return pathlen, nil
		}
	}

	// Failed access, so every path component to the right of "pathcut" needs to be examined
	for newcut := pathcut + 1; newcut < pathlen; newcut++ {
		fsysNew, err = try1(fsys, pmid(name, pathcut, newcut), name)
		if err != nil { // this is a true FNF situation
			return -1, err
		} else if fsysNew != nil {
			fsys = fsysNew
			w.burrows[pleft(name, newcut)] = fsysNew
			pathcut = newcut
			goto tryAgainWithANewlyDiscoveredImage
		}
	}
	return -1, fs.ErrNotExist
}

func try1(fsys fs.FS, name string, uniq string) (fs.FS, error) {
	s, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}

	if s.IsDir() {
		return nil, nil
	}

	// Regular-file... could be a disk image?
	o, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	subdir, _ := couldItBe(o.(io.ReaderAt))
	if subdir == nil { // nope, it's just a file
		o.Close()
		return nil, nil
	}
	subdir = &reader2readerat.FS{FS: subdir, Uniq: uniq}
	return subdir, nil
}

func couldItBe(file io.ReaderAt) (fs.FS, string) {
	var magic [16]byte
	if n, _ := file.ReadAt(magic[:], 0); n < 16 {
		return nil, ""
	}
	switch {
	case string(magic[:2]) == "ER": // Apple Partition Map
		fsys, err := apm.New(file)
		if err == nil {
			return fsys, "Apple Partition Map"
		}
	case string(magic[:2]) == "PK": // Zip file (kinda, it's complicated)
		fsys, err := zip.NewReader(file, size(file))
		if err == nil {
			return fsys, "ZIP archive"
		}
	case string(magic[10:14]) == "rLau" || string(magic[:16]) == "StuffIt (c)1997-":
		fsys, err := sit.New(file)
		if err == nil {
			return fsys, "StuffIt archive"
		}
	}

	if n, _ := file.ReadAt(magic[:2], 1024); n < 2 {
		return nil, ""
	}
	if string(magic[:2]) == "BD" {
		view, err := hfs.New(file)
		if err == nil {
			return view, "HFS"
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
