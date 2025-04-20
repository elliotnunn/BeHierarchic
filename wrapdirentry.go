package main

import (
	"io/fs"
	"time"
)

type mountPointEntry struct {
	diskImageStat fs.FileInfo
}

func (mp mountPointEntry) Name() string { // FileInfo + DirEntry
	return mp.diskImageStat.Name()
}

func (mp mountPointEntry) IsDir() bool { // FileInfo + DirEntry
	return true
}

func (mp mountPointEntry) Type() fs.FileMode { // DirEntry
	return fs.ModeDir
}

func (mp mountPointEntry) Info() (fs.FileInfo, error) { // DirEntry
	return mp, nil
}

func (mp mountPointEntry) Size() int64 { // FileInfo
	return 0 // meaningless for a directory
}

func (mp mountPointEntry) Mode() fs.FileMode { // FileInfo
	m := mp.diskImageStat.Mode()
	m = (m &^ fs.ModeType) | fs.ModeDir
	readbits := m & 0o444
	execbits := readbits >> 2
	m |= execbits
	return m
}

func (mp mountPointEntry) ModTime() time.Time { // FileInfo
	return mp.diskImageStat.ModTime()
}

func (mp mountPointEntry) Sys() any { // FileInfo
	return mp.diskImageStat.Sys()
}
