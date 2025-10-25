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

func (o path) Open() (fs.File, error)          { return o.fsys.Open(o.name.String()) }
func (o path) Stat() (fs.FileInfo, error)      { return fs.Stat(o.fsys, o.name.String()) }
func (o path) ReadDir() ([]fs.DirEntry, error) { return fs.ReadDir(o.fsys, o.name.String()) }
func (o path) Join(p string) path              { o.name = o.name.Join(p); return o }

// String returns the full path to the file
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
	warps, name = warps[:len(warps)-1], warps[len(warps)-1]

	subsys := fsys.root
	for _, el := range warps {
		p := path{fsys, subsys, internpath.New(el)}
		isar, subsubsys, err := p.getArchive(true)
		if err != nil {
			return path{}, err
		} else if !isar {
			return path{}, fs.ErrNotExist
		}
		subsys = subsubsys
	}
	return path{fsys, subsys, internpath.New(name)}, nil
}
