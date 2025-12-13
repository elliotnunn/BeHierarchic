// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"iter"
)

// Walk iterates through all the paths in the filesystem, in the order they were created.
// This implies that directories are listed before their contents.
//
// It is optional to block until a call to [FS.NoMore].
func (fsys *FS) Walk(waitFull bool) iter.Seq2[string, fs.FileMode] {
	return func(yield func(string, fs.FileMode) bool) {
		i := 0
		fsys.mu.Lock()
		for {
			switch {
			case i < len(fsys.files):
				f := fsys.files[i]
				i++
				fsys.mu.Unlock()
				if !yield(f.name.String(), f.mode.Type()) {
					return
				}
				fsys.mu.Lock()
				continue
			case !waitFull || fsys.done:
				fsys.mu.Unlock()
				return
			default:
				fsys.cond.Wait()
				continue
			}
		}
	}
}
