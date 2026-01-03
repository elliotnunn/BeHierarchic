// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

// Open opens the named file.
func (fsys *FS) Open(name string) (f fs.File, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "open", Path: name, Err: err}
		}
	}()

	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, err := fsys.lookup(name, true)
	if err != nil {
		return nil, err
	}

	fileID := fileID{fsys, idx}
	if fsys.files[idx].mode.IsDir() {
		return &dir{ent: fileID, idx: idx}, nil
	} else {
		switch d := fsys.files[idx].data.(type) {
		case io.ReaderAt:
			return &rafile{
				id:            fileID,
				SectionReader: io.NewSectionReader(d, 0, fsys.files[idx].fileSize()),
			}, nil
		default:
			return &file{
				id:   fileID,
				data: d,
			}, nil
		}
	}
}

// ReadLink returns the destination of the named symbolic link.
func (fsys *FS) ReadLink(name string) (target string, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "readlink", Path: name, Err: err}
		}
	}()

	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, err := fsys.lookup(name, false)
	if err != nil {
		return "", err
	}

	if fsys.files[idx].mode.Type() != fs.ModeSymlink {
		return "", fs.ErrInvalid
	}

	return fsys.files[idx].data.(internpath.Path).String(), nil
}

// Lstat returns a FileInfo describing the named file.
// If the file is a symlink, the returned FileInfo describes the symbolic link, not the linked file.
func (fsys *FS) Lstat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, err := fsys.lookup(name, false)
	if err != nil {
		return nil, err
	}

	return &fileID{fsys, idx}, nil
}

// Stat returns a FileInfo describing the named file.
func (fsys *FS) Stat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "stat", Path: name, Err: err}
		}
	}()

	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, err := fsys.lookup(name, true)
	if err != nil {
		return nil, err
	}

	return &fileID{fsys, idx}, nil
}

func (fsys *FS) lookup(name string, followLastLink bool) (uint32, error) {
	// Must never call internpath.New(name) because if the name is nonexistent it will leak memory:
	// use internpath.Get instead
	if !fs.ValidPath(name) {
		return 0, fs.ErrInvalid
	}

	// Fast path: applies to any regular file or directory returned by [Walk]
	if iname, ok := internpath.TryMake(name); ok {
		if idx, ok := fsys.lists[iname]; ok {
			if !followLastLink || fsys.files[idx].mode.Type() != fs.ModeSymlink {
				return idx, nil
			}
		}
	}
	if name == "." || name == "" {
		panic("how did an invalid/root name not get covered by the fast path?")
	}

	// Slow path: for the sake of great simplification, ensure the FS is "complete" before this lookup
	for !fsys.done {
		fsys.cond.Wait()
	}

	var (
		symlinkLoopDetect map[uint32]struct{}
		key               internpath.Path // zero means root
		idx               uint32          // zero means root
	)
	for name != "" {
		component, remain, notlast := strings.Cut(name, "/")
		name = remain

		var ok bool
		key, ok = key.TryJoin(component)
		if !ok {
			return 0, fs.ErrNotExist
		}
		idx, ok = fsys.lists[key]
		if !ok {
			return 0, fs.ErrNotExist
		}

		// Is it a symlink that should be followed?
		if fsys.files[idx].mode.Type() == fs.ModeSymlink && (notlast || followLastLink) {
			if _, bad := symlinkLoopDetect[idx]; bad {
				return 0, fs.ErrNotExist
			}
			if symlinkLoopDetect == nil {
				symlinkLoopDetect = make(map[uint32]struct{})
			}
			symlinkLoopDetect[idx] = struct{}{}

			key = internpath.Path{} // symlink paths are relative to root
			name = path.Join(fsys.files[idx].data.(internpath.Path).String(), name)
		}
	}
	return idx, nil
}
