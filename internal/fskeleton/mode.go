// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
)

type mode uint16

// More compact representation than io/fs
const (
	bornSizeUnknown = 1 << 14

	typeRegular     = 0 << 12
	typeLink        = 1 << 12
	typeDir         = 2 << 12
	typeImplicitDir = 3 << 12
	typeBits        = 3 << 12

	permBits = 0o7777 // traditional unix bits
)

func (m mode) Type() mode {
	return m & typeBits
}

func (m mode) IsDir() bool {
	switch m.Type() {
	case typeDir, typeImplicitDir:
		return true
	}
	return false
}

func (m mode) Stdlib() fs.FileMode {
	m2 := m.StdlibType()
	if m&0o4000 != 0 {
		m2 |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		m2 |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		m2 |= fs.ModeSticky
	}
	m2 |= fs.FileMode(m & 0o777) // rwxrwxrwx
	return m2
}

func (m mode) StdlibType() fs.FileMode {
	var m2 fs.FileMode
	switch m.Type() {
	case typeRegular:
	case typeLink:
		m2 |= fs.ModeSymlink
	case typeDir, typeImplicitDir:
		m2 |= fs.ModeDir
	}
	return m2
}

func permsFromStdlib(m fs.FileMode) mode {
	var m2 mode
	if m&fs.ModeSetuid != 0 {
		m2 |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		m2 |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		m2 |= 0o1000
	}
	m2 |= mode(m & 0o777)
	return m2
}
