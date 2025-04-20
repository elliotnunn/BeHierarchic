package main

import (
	"io/fs"
)

// a wrapper for *any directory at all* that makes sub-files more like sub-directories
type fileWithReadDirFilter struct {
	fs.ReadDirFile
	filter func(*fs.DirEntry)
}

func (f fileWithReadDirFilter) ReadDir(n int) ([]fs.DirEntry, error) {
	list, err := f.ReadDirFile.ReadDir(n)
	for i := range list {
		f.filter(&list[i])
	}
	return list, err
}

type fileWithFileInfoOverride struct {
	fs.ReadDirFile
	stat fs.FileInfo
}

func (f fileWithFileInfoOverride) Stat() (fs.FileInfo, error) {
	return f.stat, nil
}
