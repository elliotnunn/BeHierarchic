package apm

import (
	"errors"
	"io"
	"io/fs"
	"time"
)

type openRoot struct {
	fsys       *FS
	listOffset int
}

func (f *openRoot) Stat() (fs.FileInfo, error) {
	return &rootStat{}, nil
}

func (f *openRoot) Read(buf []byte) (int, error) {
	return 0, errors.New("cannot read from a directory")
}

func (f *openRoot) Close() error {
	return nil
}

// To satisfy fs.ReadDirFile, has slightly tricky partial-listing semantics
func (f *openRoot) ReadDir(count int) ([]fs.DirEntry, error) {
	n := len(f.fsys.list) - f.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	for i := range list {
		list[i] = partStat{&f.fsys.list[i]}
	}
	f.listOffset += n
	return list, nil
}

type rootStat struct {
}

func (s rootStat) Name() string { // FileInfo
	return "."
}

func (s rootStat) IsDir() bool { // FileInfo
	return true
}

func (s rootStat) Size() int64 { // FileInfo
	return 0 // meaningless for a directory
}

func (s rootStat) Mode() fs.FileMode { // FileInfo
	return fs.ModeDir | 0o777
}

func (s rootStat) ModTime() time.Time { // FileInfo
	return time.Unix(0, 0)
}

func (s rootStat) Sys() any { // FileInfo
	return nil
}
