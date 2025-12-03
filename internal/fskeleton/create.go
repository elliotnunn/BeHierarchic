// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Package fskeleton attempts to factor out the common and error-prone code in different [io.FS] implementations.
// Notably, it is only useful for static filesystems where
// the whole directory tree and all metadata is known in advance.
package fskeleton

import (
	"io"
	"io/fs"
	"slices"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

func New() *FS {
	fsys := FS{root: newDir()}
	fsys.root.name = internpath.New(".")
	fsys.walkstuff.init()
	return &fsys
}

type FS struct {
	root *dirent
	walkstuff
}

type OpenFunc func(fs.File) (fs.File, error)

// CreateDir creates a directory at the specified path.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys *FS) CreateDir(name string, mode fs.FileMode, mtime time.Time, sys any) error {
	if !fs.ValidPath(name) {
		return fs.ErrInvalid
	}
	nu := newDir()
	nu.name, nu.mode, nu.modtime, nu.sys = internpath.New(name), mode&^fs.ModeType, mtime, sys
	return fsys.create(nu)
}

// createFile validates everything for one of the exported Create*() functions
func (fsys *FS) createFile(name string, order int64, data any, size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	if !fs.ValidPath(name) {
		return fs.ErrInvalid
	}
	nu := &fileent{name: internpath.New(name),
		order:   order,
		size:    size,
		mode:    mode &^ fs.ModeType,
		modtime: mtime,
		sys:     sys,
		data:    data,
	}
	err := fsys.create(nu)
	if err != nil {
		return err
	}
	fsys.walkstuff.put(name, order)
	return nil
}

// CreateErrorFile creates a regular file at the specified path,
// which always returns the error of your choice on Read (but not on Close).
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys *FS) CreateErrorFile(name string, order int64, err error, size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	fn := func() (io.Reader, error) { return nil, err }
	return fsys.createFile(name, order, fn, size, mode, mtime, sys)
}

// CreateReaderFile creates a regular file at the specified path, which implements the bare minimum of [fs.File],
// with the Close() method stubbed out.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys *FS) CreateReaderFile(name string, order int64, r func() (io.Reader, error), size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	return fsys.createFile(name, order, r, size, mode, mtime, sys)
}

// CreateReadCloserFile creates a regular file at the specified path, which implements the bare minimum of [fs.File].
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys *FS) CreateReadCloserFile(name string, order int64, r func() (io.ReadCloser, error), size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	return fsys.createFile(name, order, r, size, mode, mtime, sys)
}

// CreateReadCloserFile creates a regular file at the specified path, which additionally implements [io.ReaderAt].
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
func (fsys *FS) CreateReaderAtFile(name string, order int64, r io.ReaderAt, size int64, mode fs.FileMode, mtime time.Time, sys any) error {
	return fsys.createFile(name, order, r, size, mode, mtime, sys)
}

// CreateSymlink creates a symbolic link at the specified path.
//
// In common with the other Create*() functions, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.CreateDir].
//
// The target argument must be an absolute path satisfying [fs.ValidPath].
//
// mode, mtime and sys are returned by the corresponding methods of [fs.FileInfo].
// There is no need to set the the [fs.ModeSymlink] bit.
func (fsys *FS) CreateSymlink(name, target string, mode fs.FileMode, mtime time.Time, sys any) error {
	if !fs.ValidPath(name) || !fs.ValidPath(target) {
		return fs.ErrInvalid
	}
	nu := &linkent{name: internpath.New(name),
		target: internpath.New(target), mode: mode &^ fs.ModeType, modtime: mtime, sys: sys}
	return fsys.create(nu)
}

// NoMoreChildren prevents future Create*() calls from adding immediate children to the specified directory.
// Future Create*() calls on this directory will fail with an error wrapping [fs.ErrPermission].
//
// NoMoreChildren unblocks any blocked [fs.ReadDirFile.ReadDir] calls on the specified directory.
//
// As a special case, if ".." is specified, then CreateDir on the root "." will be ignored.
func (fsys *FS) NoMoreChildren(name string) error {
	if name == ".." {
		fsys.root.iCond.L.Lock()
		fsys.root.makeExplicit()
		fsys.root.iCond.Broadcast()
		fsys.root.iCond.L.Unlock()
		return nil
	}

	if !fs.ValidPath(name) {
		return fs.ErrInvalid
	}

	at := fsys.root
	for _, c := range components(internpath.New(name)) {
		var err error
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
func (fsys *FS) NoMore() {
	fsys.walkstuff.done()
	fsys.root.iCond.L.Lock()
	fsys.root.makeExplicit()
	fsys.root.iCond.Broadcast()
	fsys.root.iCond.L.Unlock()
	fsys.root.noMore(true)
}

type node interface {
	fs.DirEntry
	fs.FileInfo
	pathname() internpath.Path
	open() (fs.File, error)
}

func (fsys *FS) create(node node) error {
	comps := components(node.pathname())
	if len(comps) == 0 {
		if dir, ok := node.(*dirent); ok {
			return fsys.root.replace(dir)
		} else {
			return fs.ErrExist
		}
	}

	at := fsys.root
	for _, c := range comps[:len(comps)-1] {
		var err error
		at, err = at.implicitSubdir(c)
		if err != nil {
			return err
		}
	}
	return at.put(node)
}

func components(name internpath.Path) []internpath.Path {
	var c []internpath.Path
	root := internpath.New(".")
	for cur := name; cur != root; cur = cur.Dir() {
		c = append(c, cur)
	}
	slices.Reverse(c)
	return c
}
