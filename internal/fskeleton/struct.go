// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

// FS is safe for concurrent use from multiple goroutines. It should not be copied after creation.
type FS struct {
	cond  sync.Cond  // this
	mu    sync.Mutex // points to this
	files []f
	lists map[internpath.Path]uint32
	done  bool
}

type f struct {
	id   int64
	time int64

	data any // io.ReaderAt
	// or func() (io.Reader, error)
	// or func() (io.ReadCloser, error)
	// or error
	// or internpath.Path // symlink target

	name      internpath.Path
	bozo      uint16
	mode      mode   // packed format, different from io/fs.FileMode
	lastChild uint32 // overloaded for regular files: contains the size
	sibling   uint32 // circular linked list
}

func (fsys *FS) sanityCheck() {
	if len(fsys.lists) != len(fsys.files) {
		panic("length mismatch")
	}
	for k, v := range fsys.lists {
		if k != fsys.files[v].name {
			panic("name mismatch")
		}
	}
	for _, f := range fsys.files {
		if f.mode.IsDir() && f.lastChild != 0 {
			idx := f.lastChild
			circ := make(map[uint32]bool)
			for {
				idx = fsys.files[idx].sibling
				if idx == f.lastChild {
					break // reached an appropriate circle
				}
				if circ[idx] {
					panic("circular siblings")
				}
				circ[idx] = true
			}
		}
	}
}
