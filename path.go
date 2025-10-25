package main

import (
	"io/fs"
	gopath "path"
	"slices"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

// A generalisation of a "file path"
// - specifies the public containing *FS, the hidden sub-FS, and the path within that FS
// - suitable for use as a map key
// - common operations are fast (Open, Stat, ReadDir)
// - rarer operations are possible (specifically String to get full path)
type path struct {
	container *FS
	fsys      fs.FS
	name      internpath.Path
}

// ShallowJoin returns a path with some elements added. Caution! It is only a lexical operation,
// and will return an unusable path if passed a Special character
func (o path) ShallowJoin(p string) path { o.name = o.name.Join(p); return o }

// Open opens the raw file (no archive-browsing decorations) for the benefit of reader2readerat
func (o path) Open() (fs.File, error) { return o.fsys.Open(o.name.String()) }

// String returns the full path to the file (at some small cost)
func (o path) String() string {
	o.container.rMu.RLock()
	defer o.container.rMu.RUnlock()
	warps := []string{o.name.String()}
	for o.fsys != o.container.root {
		o = o.container.reverse[o.fsys]
		warps = append(warps, o.name.String()+Special)
	}
	slices.Reverse(warps)
	return gopath.Join(warps...)
}

// path turns a string into our internal path representation
//
// Nonexistent paths might, but won't always, return fs.ErrNotExist
func (fsys *FS) path(name string) (path, error) {
	warps := strings.Split(name, Special+"/")
	if strings.HasSuffix(name, Special) {
		warps[len(warps)-1] = strings.TrimSuffix(warps[len(warps)-1], Special)
		warps = append(warps, ".")
	}

	p := path{fsys, fsys.root, internpath.New(".")}
	for _, el := range warps[:len(warps)-1] {
		var isar bool
		var err error
		isar, p, err = p.ShallowJoin(el).getArchive(true)
		if err != nil {
			return path{}, err
		} else if !isar {
			return path{}, fs.ErrNotExist
		}
	}
	return p.ShallowJoin(warps[len(warps)-1]), nil
}
