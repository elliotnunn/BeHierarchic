package main

import (
	"cmp"
	"io"
	"io/fs"
	gopath "path"
	"slices"
	"strings"
)

func (d *dir) ReadDir(count int) ([]fs.DirEntry, error) {
	if d.lseek == 0 {
		listing, err := d.fsys.ReadDir(d.name)
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

type dirEntry struct {
	fsys *FS
	name string
}

func (de *dirEntry) Name() string               { return gopath.Base(de.name) }
func (de *dirEntry) Info() (fs.FileInfo, error) { return de.fsys.Stat(de.name) }
func (de *dirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (de *dirEntry) IsDir() bool                { return true }

func (fsys *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Cases to cover:
	// - all files must implement io.ReaderAt
	// - all directories must have mountpoints added to their listing
	name, suppressSpecialSiblings := checkAndDeleteComponent(name, ".nodeeper")

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	o, err := fsys.path(name)
	if err != nil {
		return nil, err
	}
	listing, err := o.ReadDir()
	if err != nil {
		return nil, err
	}

	if suppressSpecialSiblings {
		return listing, nil
	}

	answers := make(chan *dirEntry)

	n := 0
	for _, l := range listing {
		if l.IsDir() || strings.HasPrefix(l.Name(), "._") {
			continue // no to AppleDouble files, no to directories
		}

		go func() {
			isar, _, _ := fsys.getArchive(o.Join(l.Name()), false)
			if isar {
				answers <- &dirEntry{fsys: fsys, name: gopath.Join(name, l.Name()+Special)}
			} else {
				answers <- nil
			}
		}()
		n++
	}

	for range n {
		l := <-answers
		if l != nil {
			listing = append(listing, l)
		}
	}

	slices.SortFunc(listing, func(a, b fs.DirEntry) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	return listing, nil
}
