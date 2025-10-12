package walk

import (
	"io/fs"
	"syscall"
)

func init() { tryInode = unixInode }

func unixInode(i fs.FileInfo) (uint64, bool) {
	switch t := i.Sys().(type) {
	case *syscall.Stat_t:
		return t.Ino, true
	default:
		return 0, false
	}
}
