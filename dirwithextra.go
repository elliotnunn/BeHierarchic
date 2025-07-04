// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io"
	"io/fs"
	"path"
)

type dirWithExtraChildren struct {
	fs.ReadDirFile
	parentTree *w
	ownPath    string
	listing    []fs.DirEntry
	listOffset int
}

func (f *dirWithExtraChildren) ReadDir(count int) ([]fs.DirEntry, error) {
	if f.listOffset == 0 {
		l, err := f.ReadDirFile.ReadDir(-1)
		if err != nil {
			return nil, err
		}
		for _, de := range l {
			f.listing = append(f.listing, de) // the real one
			more, err := f.parentTree.listSpecialSiblings(path.Join(f.ownPath, de.Name()))
			if err != nil {
				panic("why would this ever happen?")
			}
			for _, name := range more {
				f.listing = append(f.listing, &dirEntry{name: name})
			}
		}
	}

	// Implement those tricky partial-listing semantics
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
