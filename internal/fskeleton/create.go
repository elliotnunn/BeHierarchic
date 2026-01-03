// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Package fskeleton attempts to factor out the common code needed to implement [fs.FS].
// It is optimised for small memory usage at rest.
package fskeleton

import (
	"io"
	"io/fs"
	"slices"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

func New() *FS {
	var fsys FS
	fsys.cond.L = &fsys.mu
	fsys.files = []f{{
		name: internpath.Path{},
		mode: implicitDir,
	}}
	fsys.lists = map[internpath.Path]uint32{
		{}: 0,
	}
	return &fsys
}

func (fsys *FS) put(parentIdx uint32, f f) uint32 {
	childIdx := uint32(len(fsys.files))
	fsys.files = append(fsys.files, f)
	fsys.lists[f.name] = childIdx
	if fsys.files[parentIdx].lastChild == 0 { // only child
		fsys.files[childIdx].sibling = childIdx // circular linked list
		fsys.files[parentIdx].lastChild = childIdx
	} else { // not only child
		preceding := fsys.files[parentIdx].lastChild
		fsys.files[childIdx].sibling = fsys.files[preceding].sibling
		fsys.files[preceding].sibling = childIdx
		fsys.files[parentIdx].lastChild = childIdx
	}
	return childIdx
}

// ensureParentsExist makes certain that every containing directory exists,
// returning the index of the immediate parent.
//
// The lock must be held! Can return ErrExist if there is a non-directory in the tree
func (fsys *FS) ensureParentsExist(name internpath.Path) (uint32, error) {
	if name == (internpath.Path{}) {
		panic("this does not apply to root")
	}

	tomake := make([]internpath.Path, 0, 16)
	var parentIdx uint32
	for {
		name = name.Dir()
		var ok bool
		parentIdx, ok = fsys.lists[name]
		if ok {
			if !fsys.files[parentIdx].mode.IsDir() {
				return 0xffffffff, fs.ErrExist
			}
			break
		} else {
			tomake = append(tomake, name)
		}
	}

	for _, name := range slices.Backward(tomake) {
		parentIdx = fsys.put(parentIdx, f{
			name: name,
			mode: implicitDir,
		})
	}
	return parentIdx, nil
}

// Mkdir creates a directory at the specified path.
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
func (fsys *FS) Mkdir(name string, id int64, mode fs.FileMode, mtime time.Time) error {
	iname := internpath.Make(name)
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	if fsys.done {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrClosed}
	}

	mode = mode&^fs.ModeType | fs.ModeDir

	if idx, exist := fsys.lists[iname]; exist {
		if fsys.files[idx].mode != implicitDir {
			return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrExist}
		}
		fsys.files[idx].mode = mode
		fsys.files[idx].time = timeFromStdlib(mtime)
		fsys.files[idx].id = id
		fsys.cond.Broadcast()
		return nil
	} else {
		parentIdx, err := fsys.ensureParentsExist(iname)
		if err != nil {
			return &fs.PathError{Op: "mkdir", Path: name, Err: err}
		}
		fsys.put(parentIdx, f{
			name: iname,
			time: timeFromStdlib(mtime),
			mode: mode,
			id:   id,
		})
		fsys.cond.Broadcast()
		return nil
	}
}

// CreateError creates a regular file at the specified path.
//
// When opened, the file will always returns the specified error from [fs.File.Read]. [fs.File.Close] will have no effect.
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
func (fsys *FS) CreateError(name string, id int64, err error, size int64, mode fs.FileMode, mtime time.Time) error {
	return fsys.createRegularFileCommon(name, id, err, size, mode, mtime)
}

// CreateReader creates a regular file at the specified path.
//
// When opened, the file will implement the bare minimum of [fs.File]. [fs.File.Close] will have no effect.
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
func (fsys *FS) CreateReader(name string, id int64, r func() (io.Reader, error), size int64, mode fs.FileMode, mtime time.Time) error {
	return fsys.createRegularFileCommon(name, id, r, size, mode, mtime)
}

// CreateReadCloser creates a regular file at the specified path.
//
// When opened, the file will implement the bare minimum of [fs.File].
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
func (fsys *FS) CreateReadCloser(name string, id int64, r func() (io.ReadCloser, error), size int64, mode fs.FileMode, mtime time.Time) error {
	return fsys.createRegularFileCommon(name, id, r, size, mode, mtime)
}

// CreateReadCloser creates a regular file at the specified path.
//
// When opened, the file will satisfy [io.ReaderAt] and [io.ReadSeeker].
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
func (fsys *FS) CreateReaderAt(name string, id int64, r io.ReaderAt, size int64, mode fs.FileMode, mtime time.Time) error {
	return fsys.createRegularFileCommon(name, id, r, size, mode, mtime)
}

func (fsys *FS) createRegularFileCommon(name string, id int64, data any, size int64, mode fs.FileMode, mtime time.Time) error {
	if data == nil {
		if size != 0 {
			return &fs.PathError{Op: "create", Path: name, Err: fs.ErrInvalid}
		}
		data = io.EOF
	}

	iname := internpath.Make(name)
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	if fsys.done {
		return &fs.PathError{Op: "create", Path: name, Err: fs.ErrClosed}
	}

	if _, exist := fsys.lists[iname]; exist {
		return &fs.PathError{Op: "create", Path: name, Err: fs.ErrExist}
	}

	parentIdx, err := fsys.ensureParentsExist(iname)
	if err != nil {
		return &fs.PathError{Op: "create", Path: name, Err: err}
	}

	fsys.put(parentIdx, f{
		name:      iname,
		time:      timeFromStdlib(mtime),
		mode:      mode &^ fs.ModeType,
		id:        id,
		lastChild: packFileSize(size), // overloaded field
		data:      data,
	})
	fsys.cond.Broadcast()
	return nil
}

// Symlink creates a symbolic link at the specified path.
//
// In common with the other new-file methods, any missing parent directories will be created implicitly.
// Implicit directories can later be made explicit (only once) with [FS.Mkdir].
//
// The target argument must be an absolute path satisfying [fs.ValidPath].
func (fsys *FS) Symlink(name string, id int64, target string, mode fs.FileMode, mtime time.Time) error {
	if !fs.ValidPath(name) || !fs.ValidPath(target) {
		return fs.ErrInvalid
	}

	iname := internpath.Make(name)
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	if fsys.done {
		return &fs.PathError{Op: "link", Path: name, Err: fs.ErrClosed}
	}

	if _, exist := fsys.lists[iname]; exist {
		return &fs.PathError{Op: "link", Path: name, Err: fs.ErrExist}
	}

	parentIdx, err := fsys.ensureParentsExist(iname)
	if err != nil {
		return &fs.PathError{Op: "link", Path: name, Err: err}
	}

	fsys.put(parentIdx, f{
		name: iname,
		time: timeFromStdlib(mtime),
		mode: mode&^fs.ModeType | fs.ModeSymlink,
		id:   id,
		data: internpath.Make(target),
	})
	fsys.cond.Broadcast()
	return nil
}

// NoMore prevents all future Create*() calls, which will fail with an error wrapping [fs.ErrPermission].
//
// NoMore unblocks any blocked [fs.ReadDirFile.ReadDir] calls.
func (fsys *FS) NoMore() {
	fsys.mu.Lock()
	fsys.done = true
	fsys.cond.Broadcast()
	fsys.mu.Unlock()
}
