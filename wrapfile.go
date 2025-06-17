package main

import (
	"io"
	"io/fs"
)

type dirWithExtraChildren struct {
	fs.ReadDirFile
	extraChildren func([]fs.DirEntry) []fs.DirEntry
	listing       []fs.DirEntry
	listOffset    int
}

// Has slightly tricky partial-listing semantics
func (f *dirWithExtraChildren) ReadDir(count int) ([]fs.DirEntry, error) {
	if f.listOffset == 0 {
		l, err := f.ReadDirFile.ReadDir(-1)
		if err != nil {
			return nil, err
		}
		f.listing = append(l, f.extraChildren(l)...)
	}

	n := len(f.listing) - f.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	copy(list, f.listing[f.listOffset:][:n])
	f.listOffset += n
	return list, nil
}
