// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"io/fs"
	"time"
)

func (fi *file) Name() string       { return fi.f.Name }
func (fi *file) Size() int64        { return fi.f.Size }
func (fi *file) Mode() fs.FileMode  { return fi.f.Mode }
func (fi *file) ModTime() time.Time { return fi.f.ModTime }
func (fi *file) IsDir() bool        { return fi.f.Mode.IsDir() }
func (fi *file) Sys() any           { return fi.f.Sys }

var _ fs.FileInfo = new(file) // check satisfies interface
