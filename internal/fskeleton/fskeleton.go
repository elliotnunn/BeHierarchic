// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Package fskeleton attempts to factor out the common and error-prone code in different [io.FS] implementations.
// Notably, it is only useful for static filesystems where
// the whole directory tree and all metadata is known in advance.
package fskeleton

import (
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

type FS []file

type File struct {
	// Vital statistics
	Name    string
	Mode    fs.FileMode
	ModTime time.Time
	Size    int64
	Sys     any

	// For directories: where the children can be found in the tree argument to Make
	MapKey any

	// For symlinks: this path must be absolute in the io/fs sense
	Link string

	// For files: a function to open the file, called by FS.Open()
	FileOpener interface {
		Open(fs.File) (fs.File, error)
	}
}

// Make creates an [FS] from a directory hierarchy expressed as a map:
// each key is a unique ID for the parent directory (any type),
// and the corresponding value is an array of children.
//
// The root directory should be the sole element of tree[nil], otherwise Make will panic.
func Make(tree map[any][]File) FS {
	if len(tree[nil]) != 1 {
		panic("invalid file tree: len(tree[nil]) should be 1, got " + strconv.Itoa(len(tree[nil])))
	}

	type fixup struct {
		first, n int
		mapKey   any
	}

	var (
		list   = FS{file{f: tree[nil][0]}}
		fixups = []fixup{{mapKey: tree[nil][0].MapKey}}
	)

	for i := 0; i < len(list); i++ {
		if !list[i].f.Mode.IsDir() || list[i].f.MapKey == nil {
			continue
		}
		children := tree[fixups[i].mapKey]
		slices.SortFunc(children, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
		fixups[i].first, fixups[i].n = len(list), len(children)
		for _, e := range children {
			list = append(list, file{f: e})
			fixups = append(fixups, fixup{mapKey: e.MapKey})
		}
	}
	for i, fixup := range fixups {
		list[i].f.MapKey = nil
		if fixup.n > 0 {
			list[i].children = list[fixup.first:][:fixup.n]
		}
	}
	list[0].f.Name = "."
	return list
}

// Open retrieves a file from the tree.
// If the file is a directory, the result will also satisfy [fs.ReadDirFile].
// Otherwise, the [fs.File] will be drawn from the FileOpener passed to [Make] at filesystem creation.
//
// Using a callback to generate the actual file object might seem awkward,
// but it allows returning file objects that implement additional methods, e.g. ReadAt.
//
// Open is safe for concurrent use by multiple goroutines.
func (l FS) Open(name string) (_ fs.File, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "open", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	f, err := l.lookup(name, true)
	if err != nil {
		return nil, err
	}
	if f.f.Mode.IsDir() {
		return &lister{file: f}, nil
	} else if f.f.FileOpener == nil {
		return f, nil
	} else {
		return f.f.FileOpener.Open(f)
	}
}

func (l FS) Stat(name string) (_ fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "stat", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	f, err := l.lookup(name, true)
	if err != nil {
		return nil, err
	}
	return f, nil
}
func (l FS) ReadLink(name string) (_ string, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "readlink", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return "", fs.ErrInvalid
	}
	f, err := l.lookup(name, false)
	if err != nil {
		return "", err
	}
	if f.f.Mode&fs.ModeSymlink == 0 {
		return "", fs.ErrInvalid
	}
	return f.f.Link, nil
}

func (l FS) Lstat(name string) (_ fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	f, err := l.lookup(name, false)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (l FS) String() string {
	var s []byte
	parent := make(map[*file]string)
	for i := range l {
		f := &l[i]
		name := path.Join(parent[f], f.f.Name)

		s = append(s, []byte(f.f.Mode.String())...)
		s = append(s, ' ')
		s = f.f.ModTime.AppendFormat(s, "2006-01-02 15:04:05")
		s = append(s, ' ')
		s = append(s, name...)
		if f.f.Mode&fs.ModeSymlink != 0 {
			s = append(s, " -> "...)
			s = append(s, []byte(f.f.Link)...)
		}
		s = append(s, '\n')

		for i := range f.children {
			parent[&f.children[i]] = name
		}
	}
	if len(s) > 0 {
		s = s[:len(s)-1]
	}
	return string(s)
}

func (l FS) lookup(name string, followLastLink bool) (*file, error) {
	f := &l[0]
	components := splitComponents(name)
	for len(components) > 0 {
		c := components[0]
		components = components[1:]

		foundAt, ok := slices.BinarySearchFunc(f.children, c, func(e file, s string) int { return strings.Compare(e.f.Name, s) })
		if !ok {
			return nil, fs.ErrNotExist
		}
		f = &f.children[foundAt]

		if f.f.Mode&fs.ModeSymlink != 0 && (len(components) > 0 || followLastLink) {
			if f.f.Link == "" {
				return nil, fs.ErrNotExist
			}
			f := &l[0]                                                    // ascend back to the root
			components = append(splitComponents(f.f.Link), components...) // and squash the remaining path on the end
		}
	}
	return f, nil
}

func splitComponents(s string) []string {
	if s == "." {
		return nil
	}
	return strings.Split(s, "/")
}

// Our internal representation of a node in the tree
type file struct {
	f        File
	children []file
}
