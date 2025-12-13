// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
)

// An Open()ed directory
type dir struct {
	ent       fileID
	listIndex uint32
}

func (d *dir) ReadDir(count int) (slice []fs.DirEntry, err error) {
	errAtEnd := io.EOF
	if count <= 0 { // "read to end and don't give me EOF"
		errAtEnd = nil
	}

	fsys := d.ent.fsys
	fsys.mu.Lock()
	defer fsys.mu.Unlock()

	for {
		next := fsys.files[d.listIndex].n1 // first file in the dir
		if d.listIndex != d.ent.index {    // or subsequent file
			next = fsys.files[d.listIndex].sibling
		}

		if next == 0 { // no file
			if fsys.done { // and never will be
				return slice, errAtEnd
			} else if len(slice) == 0 || count <= 0 { // block until there is more
				fsys.cond.Wait()
				continue
			} else { // just return progress and let caller come back for more
				return slice, nil
			}
		}

		d.listIndex = next
		slice = append(slice, &fileID{fsys, next})
		if len(slice) == count {
			return slice, nil
		}
	}
}

func (d *dir) Read([]byte) (int, error)   { return 0, fs.ErrInvalid }
func (d *dir) Close() error               { return nil }
func (d *dir) Stat() (fs.FileInfo, error) { return &d.ent, nil }
