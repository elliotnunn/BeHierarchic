package fileid

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path"
	"syscall"
	"unsafe"

	"github.com/cespare/xxhash/v2"
)

func Get(fsys fs.FS, pathname string) (ID, error) {
	// Use statx to get access to the birth time of the file
	// unfortunately this forces us into some awkward interactions with io/fs
	// specifically to make sure we don't try to retrieve a symlink
	inf, err := fs.Lstat(fsys, pathname)
	if err != nil {
		return ID{}, err
	}
	if inf.Mode().Type() == fs.ModeSymlink {
		return ID{}, errors.New("is a symlink")
	}
	if _, isos := inf.Sys().(*syscall.Stat_t); !isos {
		return ID{}, ErrNotOS
	}

	f, err := fsys.Open(pathname)
	if err != nil {
		return ID{}, err
	}
	defer f.Close()

	osf, ok := f.(*os.File)
	if !ok {
		return ID{}, ErrNotOS
	}

	conn, err := osf.SyscallConn()
	if err != nil {
		return ID{}, err
	}

	var stat statx_t
	var inerr error
	err = conn.Control(func(fd uintptr) {
		inerr = statx(fd, "",
			at_empty_path|at_statx_force_sync,
			statx_btime|statx_mtime|statx_ino,
			&stat)
	})
	if err != nil {
		return ID{}, err
	} else if inerr != nil {
		return ID{}, err
	}

	var id ID

	// ID = (64 bits of inode number) + (32 bits of hash of (creation time + filename))
	binary.BigEndian.PutUint64(id[:], stat.Ino)
	var h xxhash.Digest
	binary.Write(&h, binary.BigEndian, stat.Btime.Sec)
	binary.Write(&h, binary.BigEndian, uint32(stat.Btime.Nsec))
	h.WriteString(path.Base(pathname))
	binary.BigEndian.PutUint32(id[8:], uint32(h.Sum64()))

	return id, nil
}

const (
	at_symlink_nofollow   = 0x100      /* do not follow symboliclinks. */
	at_symlink_follow     = 0x400      /* follow symbolic links. */
	at_no_automount       = 0x800      /* suppress terminal automounttraversal. */
	at_empty_path         = 0x1000     /* allow empty relative pathname to operate on dirfd directly. */
	at_statx_sync_type    = 0x6000     /* type of synchronisation required from statx() */
	at_statx_sync_as_stat = 0x0000     /* - do whatever stat() does */
	at_statx_force_sync   = 0x2000     /* - force the attributes to be sync'd with the server */
	at_statx_dont_sync    = 0x4000     /* - don't sync attributes with the server */
	statx_type            = 0x00000001 /* want/got stx_mode & s_ifmt */
	statx_mode            = 0x00000002 /* want/got stx_mode & ~s_ifmt */
	statx_nlink           = 0x00000004 /* want/got stx_nlink */
	statx_uid             = 0x00000008 /* want/got stx_uid */
	statx_gid             = 0x00000010 /* want/got stx_gid */
	statx_atime           = 0x00000020 /* want/got stx_atime */
	statx_mtime           = 0x00000040 /* want/got stx_mtime */
	statx_ctime           = 0x00000080 /* want/got stx_ctime */
	statx_ino             = 0x00000100 /* want/got stx_ino */
	statx_size            = 0x00000200 /* want/got stx_size */
	statx_blocks          = 0x00000400 /* want/got stx_blocks */
	statx_basic_stats     = 0x000007ff /* the stuff in the normal stat struct */
	statx_btime           = 0x00000800 /* want/got stx_btime */
	statx_mnt_id          = 0x00001000 /* got stx_mnt_id */
	statx_dioalign        = 0x00002000 /* want/got direct i/o alignment info */
	statx_mnt_id_unique   = 0x00004000 /* want/got extended stx_mount_id */
	statx_subvol          = 0x00008000 /* want/got stx_subvol */
	statx_write_atomic    = 0x00010000 /* want/got atomic_write_* fields */
)

func statx(dirfd uintptr, path string, flags uintptr, mask uintptr, stat *statx_t) (err error) {
	var _p0 *byte
	_p0, err = syscall.BytePtrFromString(path)
	if err != nil {
		return
	}
	_, _, e1 := syscall.Syscall6(332,
		dirfd,
		uintptr(unsafe.Pointer(_p0)),
		flags,
		mask,
		uintptr(unsafe.Pointer(stat)),
		0)
	if e1 != 0 {
		return e1
	}
	return nil
}

type statx_t struct {
	Mask       uint32
	Blksize    uint32
	Attributes uint64
	Nlink      uint32
	Uid        uint32
	Gid        uint32
	Mode       uint16

	Ino                       uint64
	Size                      uint64
	Blocks                    uint64
	Attributes_mask           uint64
	Atime                     statx_timestamp
	Btime                     statx_timestamp
	Ctime                     statx_timestamp
	Mtime                     statx_timestamp
	Rdev_major                uint32
	Rdev_minor                uint32
	Dev_major                 uint32
	Dev_minor                 uint32
	Mnt_id                    uint64
	Dio_mem_align             uint32
	Dio_offset_align          uint32
	Subvol                    uint64
	Atomic_write_unit_min     uint32
	Atomic_write_unit_max     uint32
	Atomic_write_segments_max uint32
	Dio_read_offset_align     uint32
	Atomic_write_unit_max_opt uint32
}

type statx_timestamp struct {
	Sec  int64
	Nsec uint32
}
