package main

import (
	"cmp"
	"io"
	"io/fs"
	"slices"
)

func (fsys *FS) ReadDir(name string) (list []fs.DirEntry, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	o, err := fsys.path(name)
	if err != nil {
		return nil, err
	}

	return o.cookedReadDir()
}

func (o path) rawReadDir() ([]fs.DirEntry, error) { return fs.ReadDir(o.fsys, o.name.String()) }
func (o path) cookedReadDir() ([]fs.DirEntry, error) {
	// Cases to cover:
	// - all files must return a real, positive value for Info().Size()
	// - add mountpoints to the listing
	listing, err := o.rawReadDir()
	if err != nil {
		return nil, err
	}
	for i := range listing {
		listing[i] = fileDirEntry{path: o.ShallowJoin(listing[i].Name()), mode: listing[i].Type()}
	}

	answers := make(chan *mountpointDirEntry)

	n := 0
	for _, l := range listing {
		if l.IsDir() {
			continue // no to directories
		}

		go func() {
			outer := o.ShallowJoin(l.Name())
			isar, _ := outer.getArchive(true, false)
			if isar {
				answers <- &mountpointDirEntry{outer: outer}
			} else {
				answers <- nil
			}
		}()
		n++
	}

	for range n {
		l := <-answers
		if l != nil {
			listing = append(listing, *l)
		}
	}

	slices.SortFunc(listing, func(a, b fs.DirEntry) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	return listing, nil
}

func (d *dir) ReadDir(count int) ([]fs.DirEntry, error) {
	if d.lseek == 0 {
		listing, err := d.path.cookedReadDir()
		if err != nil {
			return nil, err
		}
		d.list = listing
	}

	// Implement those tricky partial-listing semantics
	n := len(d.list) - d.lseek
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	copy(list, d.list[d.lseek:][:n])
	d.lseek += n
	return list, nil
}

type fileDirEntry struct {
	path path
	mode fs.FileMode
}

func (de fileDirEntry) Name() string               { return de.path.name.Base() }
func (de fileDirEntry) Type() fs.FileMode          { return de.mode }
func (de fileDirEntry) IsDir() bool                { return de.mode.IsDir() }
func (de fileDirEntry) Info() (fs.FileInfo, error) { return de.path.cookedStat() }

type mountpointDirEntry struct{ outer path }

func (de mountpointDirEntry) Name() string      { return de.outer.name.Base() + Special }
func (de mountpointDirEntry) Type() fs.FileMode { return fs.ModeDir }
func (de mountpointDirEntry) IsDir() bool       { return true }

func (de mountpointDirEntry) Info() (fs.FileInfo, error) {
	isAr, inner := de.outer.getArchive(true, true)
	if !isAr {
		return nil, fs.ErrInvalid // vanishingly unlikely
	}
	return inner.cookedStat()
}
