// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

var _ node = new(linkent) // check satisfies interface

type linkent struct {
	name    internpath.Path
	mode    fs.FileMode
	modtime int64
	sys     any
	target  internpath.Path
}

func (l *linkent) pathname() internpath.Path { return l.name }
func (l *linkent) open() (fs.File, error)    { panic("never open symlink") }

// common to fs.DirEntry and fs.FileInfo
func (l *linkent) Name() string { return l.name.Base() }
func (l *linkent) IsDir() bool  { return false }

// fs.DirEntry
func (l *linkent) Type() fs.FileMode          { return fs.ModeSymlink }
func (l *linkent) Info() (fs.FileInfo, error) { return l, nil }

// fs.FileInfo
func (l *linkent) Size() int64        { return 0 }
func (l *linkent) Mode() fs.FileMode  { return l.mode&^fs.ModeType | fs.ModeSymlink }
func (l *linkent) ModTime() time.Time { return timeToStdlib(l.modtime) }
func (l *linkent) Sys() any           { return l.sys }

// fs.File
func (*linkent) Close() error               { panic("never open symlink") }
func (*linkent) Read([]byte) (int, error)   { panic("never open symlink") }
func (*linkent) Stat() (fs.FileInfo, error) { panic("never open symlink") }
