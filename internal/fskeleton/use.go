// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"strings"
)

func (fsys *FS) Open(name string) (f fs.File, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "open", Path: name, Err: err}
		}
	}()

	node, err := fsys.lookup(name, true)
	if err != nil {
		return nil, err
	}
	return node.open()
}

func (fsys *FS) ReadLink(name string) (target string, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "readlink", Path: name, Err: err}
		}
	}()

	node, err := fsys.lookup(name, false)
	if err != nil {
		return "", err
	}
	switch t := node.(type) {
	case *linkent:
		return t.target.String(), nil
	default:
		return "", fs.ErrInvalid
	}
}

func (fsys *FS) Lstat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	return fsys.lookup(name, false)
}

func (fsys *FS) Stat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	return fsys.lookup(name, true)
}

func (fsys *FS) lookup(name string, followLastLink bool) (node, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	var (
		components []string
		beenHere        = make(map[node]bool)
		f          node = fsys.root
		err        error
	)

	if name != "." {
		components = strings.Split(name, "/")
	}

	for len(components) > 0 {
		c := components[0]
		components = components[1:]

		dir, ok := f.(*dirent)
		if !ok {
			return nil, fs.ErrNotExist // trying to subdir a file
		}

		f, err = dir.lookup(dir.pathname().Join(c))
		if err != nil {
			return nil, err
		}

		if link, ok := f.(*linkent); ok && (len(components) > 0 || followLastLink) {
			if beenHere[f] {
				return nil, fs.ErrNotExist
			}
			beenHere[f] = true

			f = fsys.root // ascend back to the root
			ltarg := link.target.String()
			if ltarg != "." {
				components = append(strings.Split(ltarg, "/"), components...)
			}
		}
	}
	return f, nil
}
