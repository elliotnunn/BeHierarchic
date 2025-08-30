// Copyright Elliot Nunn. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tar

import (
	"io"
	"io/fs"
)

type opener struct{ io.ReaderAt }

func (f opener) Open(statter fs.File) (fs.File, error) {
	s, _ := statter.Stat()
	return file{statter, io.NewSectionReader(f, 0, s.Size())}, nil
}

type file struct {
	statter
	*io.SectionReader
}
type statter interface{ Stat() (fs.FileInfo, error) }

func (file) Close() error { return nil }

var _ fs.File = file{} // method check
