// Copyright (c) Elliot Nunn

// This library is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 2.1 of the License, or (at your option) any later version.

// This library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.

package sit

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"path"
	"testing"
)

//go:embed stuffit-test-files/build
var archivesFS embed.FS

func TestAlgorithms(t *testing.T) {
	fs.WalkDir(archivesFS, ".", func(outerpath string, d fs.DirEntry, _ error) error {
		switch path.Ext(outerpath) {
		case ".sea", ".sit":
		default:
			return nil
		}
		f, _ := archivesFS.Open(outerpath)
		t.Run(outerpath, func(t *testing.T) {
			sit, err := New(f.(io.ReaderAt))
			if err != nil {
				t.Fatal(err)
			}

			fs.WalkDir(sit, ".", func(innerpath string, d fs.DirEntry, _ error) error {
				if d.IsDir() {
					return nil
				}

				t.Run(innerpath, func(t *testing.T) {
					_, err := fs.ReadFile(sit, innerpath)
					if errors.Is(err, ErrAlgo) || errors.Is(err, ErrPassword) {
						t.Skip(err)
					} else if err != nil {
						t.Fatal(err)
					}
				})
				return nil
			})
		})
		return nil
	})
}
