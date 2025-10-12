// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"time"
	"unique"
)

var _ node = new(linkent) // check satisfies interface

type linkent struct {
	name    unique.Handle[string]
	mode    fs.FileMode
	modtime time.Time
	sys     any
	target  string
}

func (l *linkent) open() (fs.File, error) { panic("never open symlink") }

// common to fs.DirEntry and fs.FileInfo
func (l *linkent) Name() string { return l.name.Value() }
func (l *linkent) IsDir() bool  { return false }

// fs.DirEntry
func (l *linkent) Type() fs.FileMode          { return fs.ModeSymlink }
func (l *linkent) Info() (fs.FileInfo, error) { return l, nil }

// fs.FileInfo
func (l *linkent) Size() int64        { return 0 }
func (l *linkent) Mode() fs.FileMode  { return l.mode&^fs.ModeType | fs.ModeSymlink }
func (l *linkent) ModTime() time.Time { return l.modtime }
func (l *linkent) Sys() any           { return l.sys }

// fs.File
func (*linkent) Close() error               { panic("never open symlink") }
func (*linkent) Read([]byte) (int, error)   { panic("never open symlink") }
func (*linkent) Stat() (fs.FileInfo, error) { panic("never open symlink") }
