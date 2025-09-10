// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
)

type lister struct {
	*file
	progress int
}

// Tricky partial-listing semantics
func (l *lister) ReadDir(count int) ([]fs.DirEntry, error) {
	n := len(l.children) - l.progress
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	for i := range list {
		list[i] = fs.FileInfoToDirEntry(&l.children[l.progress+i])
	}
	l.progress += n
	return list, nil
}

var _ fs.ReadDirFile = new(lister) // check satisfies interface
