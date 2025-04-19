package main

import (
	"io/fs"
)

// a wrapper for *any directory at all* that makes sub-files more like sub-directories
type fsFileThatConvertsSomeSubfilesToDirectories struct {
	fs.ReadDirFile
	shouldBeADirectory func(s string) bool
}

func (f fsFileThatConvertsSomeSubfilesToDirectories) ReadDir(n int) ([]fs.DirEntry, error) {
	list, err := f.ReadDirFile.ReadDir(n)
	for i := range list {
		if !list[i].IsDir() && f.shouldBeADirectory(list[i].Name()) {
			list[i] = dirEntryThatLooksLikeAFolder{list[i]}
		}
	}
	return list, err
}
