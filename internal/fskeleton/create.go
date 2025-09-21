// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Package fskeleton attempts to factor out the common and error-prone code in different [io.FS] implementations.
// Notably, it is only useful for static filesystems where
// the whole directory tree and all metadata is known in advance.
package fskeleton

import (
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

func New() FS {
	fsys := FS{newDir()}
	fsys.root.name = "."
	return fsys
}

type FS struct{ root *dirent }

type OpenFunc func(fs.File) (fs.File, error)

// CreateDir creates a directory at the specified path.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys FS) CreateDir(name string, mode fs.FileMode, mtime time.Time, sys any) error {
	nu := newDir()
	nu.name, nu.mode, nu.modtime, nu.sys = strings.Clone(path.Base(name)), mode, mtime, sys
	nu.iOK = true
	return fsys.create(name, nu)
}

// CreateFile creates a regular file at the specified path.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys FS) CreateFile(name string, open OpenFunc, size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	nu := &fileent{name: strings.Clone(path.Base(name)),
		size: size, mode: mode, modtime: mtime, sys: sys, opener: open}
	return fsys.create(name, nu)
}

// CreateRandomAccessFile is like CreateFile, but when opened, the [fs.File] will also satisfy [io.ReadSeeker] and [io.ReaderAt].
func (fsys FS) CreateRandomAccessFile(name string, r io.ReaderAt, size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	fn := func(stub fs.File) (fs.File, error) {
		return raFile{raMetadata: stub, raData: io.NewSectionReader(r, 0, size)}, nil
	}
	nu := &fileent{name: strings.Clone(path.Base(name)),
		size: size, mode: mode, modtime: mtime, sys: sys, opener: fn}
	return fsys.create(name, nu)
}

type raMetadata interface {
	Stat() (fs.FileInfo, error)
}

type raData interface {
	Read([]byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
	ReadAt(p []byte, off int64) (n int, err error)
}

type raFile struct {
	raMetadata
	raData
}

func (raFile) Close() error { return nil }

// CreateSymlink creates a symbolic link at the specified path.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// The target argument must be an absolute path satisfying [fs.ValidPath].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
// There is no need to set the the [fs.ModeSymlink] bit.
func (fsys FS) CreateSymlink(name, target string, mode fs.FileMode, mtime time.Time, sys any) error {
	if !fs.ValidPath(target) {
		return fs.ErrInvalid
	}
	nu := &linkent{name: strings.Clone(path.Base(name)),
		target: target, mode: mode, modtime: mtime, sys: sys}
	return fsys.create(name, nu)
}

// NoMoreChildren prevents future Create*() calls from adding immediate children to the specified directory.
// Future Create*() calls on this directory will fail with an error wrapping [fs.ErrPermission].
//
// NoMoreChildren unblocks any blocked [fs.ReadDirFile.ReadDir] calls on the specified directory.
//
// As a special case, if ".." is specified, then CreateDir on the root "." will be ignored.
func (fsys FS) NoMoreChildren(name string) error {
	if name == ".." {
		fsys.root.iCond.L.Lock()
		fsys.root.iOK = true
		fsys.root.iCond.Broadcast()
		fsys.root.iCond.L.Unlock()
		return nil
	}

	comps, err := checkSplit(name)
	if err != nil {
		return err
	}

	at := fsys.root
	for _, c := range comps {
		at, err = at.implicitSubdir(c)
		if err != nil {
			return err
		}
	}
	at.noMore(false)
	return nil
}

// NoMore prevents all future Create*() calls, which will fail with an error wrapping [fs.ErrPermission].
//
// NoMore unblocks any blocked [fs.ReadDirFile.ReadDir] calls.
func (fsys FS) NoMore() {
	fsys.root.iCond.L.Lock()
	fsys.root.iOK = true
	fsys.root.iCond.Broadcast()
	fsys.root.iCond.L.Unlock()
	fsys.root.noMore(true)
}

type node interface {
	fs.DirEntry
	fs.FileInfo
	open() (fs.File, error)
}

func (fsys FS) create(name string, node node) error {
	comps, err := checkSplit(name)
	if err != nil {
		return err
	}

	if len(comps) == 0 {
		if dir, ok := node.(*dirent); ok {
			return fsys.root.replace(dir)
		} else {
			return fs.ErrExist
		}
	}

	at := fsys.root
	for _, c := range comps[:len(comps)-1] {
		at, err = at.implicitSubdir(c)
		if err != nil {
			return err
		}
	}
	return at.put(node)
}

func checkSplit(name string) ([]string, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	} else if name == "." {
		return nil, nil
	} else {
		return strings.Split(name, "/"), nil
	}
}
