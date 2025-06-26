// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package apm

import (
	"io"
	"io/fs"
	"time"
)

type openPart struct {
	*io.SectionReader
	part *partition
}

func (f *openPart) Stat() (fs.FileInfo, error) {
	return partStat{f.part}, nil
}

func (f *openPart) Close() error {
	return nil
}

type partStat struct {
	part *partition
}

func (s partStat) Name() string { // FileInfo + DirEntry
	return s.part.name
}

func (s partStat) IsDir() bool { // FileInfo + DirEntry
	return false
}

func (s partStat) Type() fs.FileMode { // DirEntry
	return 0 // regular file
}

func (s partStat) Info() (fs.FileInfo, error) { // DirEntry
	return s, nil
}

func (s partStat) Size() int64 { // FileInfo
	return s.part.len
}

func (s partStat) Mode() fs.FileMode { // FileInfo
	return 0o666
}

func (s partStat) ModTime() time.Time { // FileInfo
	return time.Unix(0, 0)
}

func (s partStat) Sys() any { // FileInfo
	return nil
}
