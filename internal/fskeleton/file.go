// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io"
	"io/fs"
)

// Simplest possible implementation of fs.File
func (*file) Close() error                 { return nil }
func (*file) Read([]byte) (int, error)     { return 0, io.EOF }
func (l *file) Stat() (fs.FileInfo, error) { return l, nil }

var _ fs.File = new(file) // check satisfies interface
