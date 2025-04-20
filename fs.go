package main

import (
	"io"
	"io/fs"
	"path"

	"github.com/elliotnunn/resourceform/internal/hfs"
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
		o = fsFileThatConvertsSomeSubfilesToDirectories{
			ReadDirFile: o.(fs.ReadDirFile),
			shouldBeADirectory: func(s string) bool {
				s = path.Join(name, s)
				pathlen := plen(s)
				pathcut, _ := w.resolve(s)
				return pathcut == pathlen
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
	fsysNew, err = try1(fsys, pright(name, pathcut))
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
		fsysNew, err = try1(fsys, pmid(name, pathcut, newcut))
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

func try1(fsys fs.FS, name string) (fs.FS, error) {
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
	return subdir, nil
}

func couldItBe(file io.ReaderAt) (fs.FS, string) {
	var magic [2]byte
	if _, err := file.ReadAt(magic[:], 1024); err == nil && string(magic[:]) == "BD" {
		view, err := hfs.New(file)
		if err == nil {
			return view, "HFS"
		}
	}
	return nil, ""
}
