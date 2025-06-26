// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"io/fs"
	"time"
)

type dirEntry struct {
	name  string
	mtime time.Time
}

func (de *dirEntry) Name() string { // FileInfo + DirEntry
	return de.name
}

func (de *dirEntry) IsDir() bool { // FileInfo + DirEntry
	return true
}

func (de *dirEntry) Type() fs.FileMode { // DirEntry
	return fs.ModeDir
}

func (de *dirEntry) Info() (fs.FileInfo, error) { // DirEntry
	return de, nil
}

func (de *dirEntry) Size() int64 { // FileInfo
	return 0 // meaningless for a directory
}

func (de *dirEntry) Mode() fs.FileMode { // FileInfo
	return fs.ModeDir | 0o755
}

func (de *dirEntry) ModTime() time.Time { // FileInfo
	return de.mtime
}

func (de *dirEntry) Sys() any { // FileInfo
	return nil
}
