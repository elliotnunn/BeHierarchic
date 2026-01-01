// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
)

// An Open()ed directory
type dir struct {
	ent  fileID
	idx  uint32
	next uint32
	last uint32
}

func (d *dir) ReadDir(count int) (slice []fs.DirEntry, err error) {
	fsys := d.ent.fsys
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	for !fsys.done { // wait for the dir to be completed before returning anything at all
		fsys.cond.Wait()
	}

	errAtEnd := io.EOF
	if count <= 0 { // "read to end and don't give me EOF"
		errAtEnd = nil
	}

	if d.next == 0xffffffff {
		return nil, errAtEnd // reached end of directory
	}

	if d.next == 0 {
		d.last = fsys.files[d.idx].lastChild
		d.next = fsys.files[d.last].sibling
		if d.next == 0 {
			d.next = 0xffffffff
			return nil, errAtEnd // empty directory
		}
	}

	for len(slice) != count {
		slice = append(slice, &fileID{fsys, d.next})
		if d.next == d.last {
			d.next = 0xffffffff
			break
		}
		d.next = fsys.files[d.next].sibling
	}
	if len(slice) == count {
		return slice, nil
	} else {
		return slice, errAtEnd
	}
}

func (d *dir) Read([]byte) (int, error)   { return 0, fs.ErrInvalid }
func (d *dir) Close() error               { return nil }
func (d *dir) Stat() (fs.FileInfo, error) { return &d.ent, nil }
