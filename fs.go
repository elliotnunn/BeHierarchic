package main

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/elliotnunn/resourceform/internal/hfs"
)

type w struct {
	burrows map[string]fs.FS
}

func Wrapper(fsys fs.FS) fs.FS {
	return &w{
		burrows: map[string]fs.FS{
			".": fsys,
		},
	}
}

// Okay, here's the tricky thing...
// FS implements Open returns File implements Stat returns FileInfo
// FS implements Open returns File implements ReadDir returns DirEntry implements Info returns FileInfo

// We need to keep a positive list of files that correspond with a burrow
// Do we need to keep a list of files that are not? Nah, too costly.
func (fsys *w) Open(name string) (retf fs.File, reterr error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	defer func() {
		// cheeky: whenever we return something, ensure it is wrapped in our fwrap struct
		// this could be *quite a bit* better
		if retf != nil && isdir(retf) {
			retf = fsFileThatConvertsSomeSubfilesToDirectories{
				ReadDirFile: retf.(fs.ReadDirFile),
				shouldBeADirectory: func(s string) bool {
					s = path.Join(name, s)
					if _, knownhole := fsys.burrows[s]; knownhole {
						return true
					}
					o, err := fsys.Open(s)
					if err != nil {
						panic(fmt.Sprintf("%e %s", err, s))
					}
					o.Close() // only needed it for the side-effect
					_, knownhole := fsys.burrows[s]
					return knownhole
				},
			}
		}
	}()

	// This is the fairly happy path
	var knownTarget fs.FS
	var right string

	for tryfind := name; ; tryfind = dirname(tryfind) {
		subfsys, ok := fsys.burrows[tryfind]
		if ok {
			knownTarget = subfsys
			right = stripPrefix(name, tryfind)
			break
		}
	}

	// Attempt access
	o, err := knownTarget.Open(right)

	// Openable... should it actually be a directory though?
	if err == nil {
		if !isdir(o) {
			ora := o.(io.ReaderAt) // we demand this of all our filesystems
			subdir, _ := couldItBe(ora)
			if subdir != nil {
				fsys.burrows[name] = subdir
				return subdir.Open(".")
			}
		}
		return o, nil
	}

	// Down here is the sad, rare path
	// Access not possible:
	// - either this is truly a FNF err
	// - or (less likely) there is an as-yet unidentified subFS in the path somewhere,
	//   so let's do some ugly recursion
	stateOfKnowledge := len(fsys.burrows)
	inferior := name
	for {
		if inferior == "." {
			break
		}
		inferior = dirname(inferior)
		so, _ := fsys.Open(inferior)
		if so != nil {
			so.Close()
			break
		}
	}
	if len(fsys.burrows) > stateOfKnowledge {
		return fsys.Open(name) // retry if not entirely futile
	} else {
		return nil, err
	}
}

func couldItBe(file io.ReaderAt) (fs.FS, string) {
	var magic [2]byte
	if _, err := file.ReadAt(magic[:], 1024); err == nil && string(magic[:]) == "BD" {
		view, err := hfs.New(file)
		if err == nil {
			return view, "HFS"
		}
	}
	return nil, ""
}

func stripPrefix(p string, prefix string) string {
	if prefix == "." {
		return p
	}
	if !strings.HasPrefix(p, prefix) || (len(p) > len(prefix) && p[len(prefix)] != '/') {
		panic(fmt.Sprintf("%q is not a path prefix of %q", prefix, p))
	}
	p = strings.TrimPrefix(p[len(prefix):], "/")
	if p == "" {
		return "."
	}
	return p
}

func dirname(s string) string {
	if s == "." {
		panic("no parent of the root")
	}
	slash := strings.IndexByte(s, '/')
	if slash == -1 {
		return "."
	}
	return s[:slash]
}

func isdir(f fs.File) bool {
	stat, err := f.Stat()
	if err != nil {
		panic("failing to stat an open file is unrecoverable")
	}
	return stat.IsDir()
}
