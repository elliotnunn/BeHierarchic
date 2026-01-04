// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"time"
)

// FileInfo will always be satisfied wherever [fs.FileInfo] is satisfied.
type FileInfo interface {
	fs.FileInfo
	ID() int64
}

type fileID struct {
	fsys  *FS
	index uint32
}

var (
	_ fs.DirEntry = new(fileID)
	_ fs.FileInfo = new(fileID)
	_ FileInfo    = new(fileID)
)

func (f *fileID) Name() string {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	return f.fsys.files[f.index].name.Base()
}

func (f *fileID) IsDir() bool {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	return f.fsys.files[f.index].mode.IsDir()
}

func (f *fileID) Type() fs.FileMode {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	return f.fsys.files[f.index].mode.StdlibType()
}

func (f *fileID) Info() (fs.FileInfo, error) { return f, nil }

func (f *fileID) Size() int64 {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	if f.fsys.files[f.index].mode.Type() != typeRegular {
		return 0
	}
	return f.fsys.files[f.index].fileSize()
}

func (f *fileID) Mode() fs.FileMode {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	for !f.fsys.done && f.fsys.files[f.index].mode.Type() == typeImplicitDir {
		f.fsys.cond.Wait()
	}
	return f.fsys.files[f.index].mode.Stdlib()
}

func (f *fileID) ModTime() time.Time {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	for !f.fsys.done && f.fsys.files[f.index].mode.Type() == typeImplicitDir {
		f.fsys.cond.Wait()
	}
	return timeToStdlib(f.fsys.files[f.index].time)
}

func (f *fileID) Sys() any { return nil }

func (f *fileID) ID() int64 {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	for !f.fsys.done && f.fsys.files[f.index].mode.Type() == typeImplicitDir {
		f.fsys.cond.Wait()
	}
	return f.fsys.files[f.index].id
}
