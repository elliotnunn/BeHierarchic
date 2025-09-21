// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
)

func (fsys FS) Open(name string) (f fs.File, err error) {
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

func (fsys FS) ReadLink(name string) (target string, err error) {
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
		return t.target, nil
	default:
		return "", fs.ErrInvalid
	}
}

func (fsys FS) Lstat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	return fsys.lookup(name, false)
}

func (fsys FS) Stat(name string) (info fs.FileInfo, err error) {
	defer func() {
		if err != nil {
			err = &fs.PathError{Op: "lstat", Path: name, Err: err}
		}
	}()

	return fsys.lookup(name, true)
}

func (fsys FS) lookup(name string, followLastLink bool) (node, error) {
	beenHere := make(map[any]struct{})
	f := node(fsys.root)
	components, err := checkSplit(name)
	if err != nil {
		return nil, err
	}
	for len(components) > 0 {
		if _, ok := beenHere[f]; ok {
			return nil, fs.ErrNotExist // circular symlink
		}
		beenHere[f] = struct{}{}

		c := components[0]
		components = components[1:]

		dir, ok := f.(*dirent)
		if !ok {
			return nil, fs.ErrNotExist // trying to subdir a file
		}

		f, err = dir.lookup(c)
		if err != nil {
			return nil, err
		}

		if link, ok := f.(*linkent); ok && (len(components) > 0 || followLastLink) {
			f = fsys.root // ascend back to the root
			lsp, err := checkSplit(link.target)
			if err != nil {
				return nil, err
			}
			components = append(lsp, components...) // and squash the remaining path on the end
		}
	}
	return f, nil
}
