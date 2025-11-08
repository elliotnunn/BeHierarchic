//go:build unix

package main

import (
	"io/fs"
	"syscall"
)

func fileID(i fs.FileInfo) (uint64, bool) {
	switch t := i.Sys().(type) {
	case *syscall.Stat_t:
		return t.Ino, true
	default:
		return 0, false
	}
}
