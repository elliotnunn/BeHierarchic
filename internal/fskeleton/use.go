// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
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
	idx, err := fsys.lookup(name, true, true)
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
	idx, err := fsys.lookup(name, true, false)
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
	idx, err := fsys.lookup(name, true, false)
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
	idx, err := fsys.lookup(name, true, true)
	if err != nil {
		return nil, err
	}

	return &fileID{fsys, idx}, nil
}

func (fsys *FS) lookup(name string, waitForever, followLastLink bool) (uint32, error) {
	if !fs.ValidPath(name) {
		return 0, fs.ErrInvalid
	}

	symlinkLoopDetect := make(map[uint32]struct{})

	iname := internpath.New(name)
retry:
	for {
		// Peel components off the end of the path until we find one that exists
		var idx uint32
		stub := iname
		for {
			var ok bool
			if idx, ok = fsys.lists[stub]; ok {
				break
			}
			stub = stub.Dir()
		}
		// now the invariant is that "stub" exists and corresponds with "idx"

		// Is it a symlink that should be followed?
		if fsys.files[idx].mode.Type() == fs.ModeSymlink && (stub != iname || followLastLink) {
			if _, bad := symlinkLoopDetect[idx]; bad {
				return 0, fs.ErrNotExist
			}
			symlinkLoopDetect[idx] = struct{}{}

			// Rewrite "iname" through string manipulation
			//    Say "a/symlink" points to "b/c"
			//    Then replace path "a/symlink/**" with "b/c/**"
			link := stub.String()
			remainder := iname.String()[len(link):]
			iname = fsys.files[idx].data.(internpath.Path) // the link target
			if len(remainder) > 0 {
				iname = iname.Join(strings.TrimPrefix(remainder, "/"))
			}
			continue retry // from the top, but with a rewritten "iname"
		}

		if stub != iname {
			// If it is possible for a future Create() call to satisfy, then wait for it
			if waitForever && !fsys.done && fsys.files[idx].mode.IsDir() {
				fsys.cond.Wait()
				continue retry
			}
			return 0, fs.ErrNotExist
		}

		return idx, nil // success
	}
}
