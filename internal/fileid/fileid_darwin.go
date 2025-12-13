package fileid

import (
	"encoding/binary"
	"io/fs"
	"path"
	"syscall"

	"github.com/cespare/xxhash/v2"
)

func Get(fsys fs.FS, pathname string) (ID, error) {
	inf, err := fs.Lstat(fsys, pathname)
	if err != nil {
		return ID{}, err
	}
	stat, ok := inf.Sys().(*syscall.Stat_t)
	if !ok {
		return ID{}, ErrNotOS
	}

	var id ID

	// Identity = (64 bits of inode number) + (32 bits of hash of (creation time + filename))
	binary.BigEndian.PutUint64(id[:], stat.Ino)
	var h xxhash.Digest
	binary.Write(&h, binary.BigEndian, stat.Birthtimespec.Sec)
	binary.Write(&h, binary.BigEndian, uint32(stat.Birthtimespec.Nsec))
	h.WriteString(path.Base(pathname))
	binary.BigEndian.PutUint32(id[8:], uint32(h.Sum64()))

	return id, nil
}
