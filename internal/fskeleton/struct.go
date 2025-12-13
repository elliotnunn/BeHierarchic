// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

const implicitDir fs.FileMode = ^fs.ModeType | fs.ModeDir // nonsense value but satisfies FileMode.IsDir()

// FS is safe for concurrent use from multiple goroutines. It should not be copied after creation.
type FS struct {
	cond  sync.Cond  // this
	mu    sync.Mutex // points to this
	files []f
	lists map[internpath.Path]uint32
	done  bool
}

type f struct {
	name    internpath.Path
	time    int64
	mode    fs.FileMode
	sibling uint32
	id      int64

	// directories: index of first and last child
	// files: packed size
	n1, n2 uint32

	data any // io.ReaderAt
	// or func() (io.Reader, error)
	// or func() (io.ReadCloser, error)
	// or error
	// or internpath.Path // symlink target
}

func (f *f) fileSize() int64 { return int64(f.n1) | int64(f.n2)<<32 }

func (fsys *FS) sanityCheck() {
	if len(fsys.lists) != len(fsys.files) {
		panic("length mismatch")
	}
	for k, v := range fsys.lists {
		if k != fsys.files[v].name {
			panic("name mismatch")
		}
	}
	for i, f := range fsys.files {
		if i != 0 && f.sibling == uint32(i) {
			panic("its own sibling!")
		}
		if f.sibling != 0 {
			sib := fsys.files[f.sibling]
			if f.name.Dir() != sib.name.Dir() {
				panic("not really a sibling")
			}
		}
		if f.mode.IsDir() {
			if (f.n1 == 0) != (f.n2 == 0) {
				panic("mismatched have-child-ness")
			}
			if f.n1 != 0 { // has a child
				if fsys.files[f.n1].name.Dir() != f.name {
					panic("first child does not have child name")
				}
				if fsys.files[f.n2].name.Dir() != f.name {
					panic("last child does not have child name")
				}

				child := f.n1
				for fsys.files[child].sibling != 0 {
					child = fsys.files[child].sibling
				}
				if child != uint32(f.n2) {
					panic("wrong last-child")
				}
			}
		}

	}
}
