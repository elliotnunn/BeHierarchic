package fskeleton

import (
	"errors"
	"io/fs"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

const SizeUnknown = -1

var ErrSizeUnknown = errors.New("file created with SizeUnknown")

// SetSize sets the size of a regular file that has already been created with [SizeUnknown].
func (fsys *FS) SetSize(name internpath.Path, size int64) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, ok := fsys.lists[name]
	if !ok {
		return fs.ErrNotExist
	}

	if fsys.files[idx].mode.Type() != typeRegular {
		return fs.ErrInvalid
	}
	if fsys.files[idx].lastChild != 0xffffffff {
		return fs.ErrInvalid
	}

	fsys.files[idx].lastChild = packFileSize(size)
	return nil
}

func (fsys *FS) Size(name internpath.Path) (int64, error) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, ok := fsys.lists[name]
	if !ok {
		return 0, fs.ErrNotExist
	}

	if fsys.files[idx].mode.Type() != typeRegular {
		return 0, fs.ErrInvalid
	}
	if fsys.files[idx].lastChild == 0xffffffff {
		return 0, ErrSizeUnknown
	}
	return fsys.files[idx].fileSize(), nil
}

func (fsys *FS) BornSizeUnknown(name internpath.Path) (bool, error) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	idx, ok := fsys.lists[name]
	if !ok {
		return false, fs.ErrNotExist
	}
	return fsys.files[idx].mode&bornSizeUnknown != 0, nil
}

// Store extremely large file sizes elsewhere
var (
	lgsizeMu sync.RWMutex
	lgsize   []int64
)

func packFileSize(size int64) uint32 {
	if size == SizeUnknown {
		return 0xffffffff
	} else if size < 0x80000000 {
		return uint32(size)
	} else {
		lgsizeMu.Lock()
		packSize := 0x80000000 + uint32(len(lgsize))
		lgsize = append(lgsize, size)
		lgsizeMu.Unlock()
		return packSize
	}
}

func (f *f) fileSize() int64 {
	packSize := f.lastChild
	if packSize == 0xffffffff {
		return SizeUnknown
	} else if packSize < 0x80000000 {
		return int64(packSize)
	} else {
		lgsizeMu.RLock()
		defer lgsizeMu.RUnlock()
		return lgsize[packSize-0x80000000]
	}
}
