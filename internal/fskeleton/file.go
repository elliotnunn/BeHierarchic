// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
	"time"
)

var _ node = new(fileent) // check satisfies interface

type fileent struct {
	name    string
	size    int64
	mode    fs.FileMode
	modtime time.Time
	sys     any
	opener  func(fs.File) (fs.File, error)
}

func (f *fileent) open() (fs.File, error) { return f.opener(f) }

// common to fs.DirEntry and fs.FileInfo
func (f *fileent) Name() string { return f.name }
func (f *fileent) IsDir() bool  { return false }

// fs.DirEntry
func (f *fileent) Type() fs.FileMode          { return 0 }
func (f *fileent) Info() (fs.FileInfo, error) { return f, nil }

// fs.FileInfo
func (f *fileent) Size() int64        { return f.size }
func (f *fileent) Mode() fs.FileMode  { return f.mode &^ fs.ModeType }
func (f *fileent) ModTime() time.Time { return f.modtime }
func (f *fileent) Sys() any           { return f.sys }

// fs.File
func (*fileent) Close() error                 { return nil }
func (*fileent) Read([]byte) (int, error)     { return 0, io.EOF }
func (f *fileent) Stat() (fs.FileInfo, error) { return f, nil }
