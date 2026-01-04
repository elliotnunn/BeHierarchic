// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import "github.com/elliotnunn/BeHierarchic/internal/internpath"

// SetBozo stores a small integer in the file info.
// It is intended for use by the consumer of the filesystem, not the creator.
func (fsys *FS) SetBozo(name internpath.Path, bozo uint16) (prev uint16, ok bool) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	for {
		idx, ok := fsys.lists[name]
		if !ok {
			if fsys.done {
				return 0, false
			}
			fsys.cond.Wait()
			continue
		}
		prev = fsys.files[idx].bozo
		fsys.files[idx].bozo = bozo
		return prev, true
	}
}

func (fsys *FS) GetBozo(name internpath.Path) (bozo uint16, ok bool) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	for {
		idx, ok := fsys.lists[name]
		if !ok {
			if fsys.done {
				return 0, false
			}
			fsys.cond.Wait()
			continue
		}
		return fsys.files[idx].bozo, true
	}
}
