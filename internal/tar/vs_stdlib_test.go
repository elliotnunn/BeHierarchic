// Copyright Elliot Nunn. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Compare this package against the canonical go one

package tar

import (
	gotar "archive/tar"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"testing"
)

//go:embed testdata
var testdata embed.FS

func TestVsStandardLibrary(t *testing.T) {
	tars, _ := fs.Glob(testdata, "testdata/*.tar")
	for _, name := range tars {
		t.Run(path.Base(name), func(t *testing.T) {
			f, _ := testdata.Open(name)

			ourFiles, _ := dumpOurImplementation(f.(io.ReaderAt))
			theirFiles, _ := dumpStdlibImplementation(f)

			// if comparableErrorString(theirErr) != comparableErrorString(ourErr) {
			// 	t.Errorf("expected error %v, got %v", theirErr, ourErr)
			// }
			// if theirErr != nil {
			// 	t.Logf("agreed on an error: %v", theirErr)
			// }

			for name, theirValue := range theirFiles {
				ourValue, ok := ourFiles[name]
				if !ok {
					t.Errorf("our implementation missing a %s: %q", strings.SplitN(theirValue, "=", 2)[0], name)
				} else if theirValue != ourValue {
					if len(theirValue) > 100 {
						theirValue = theirValue[:100] + "..."
					}
					if len(ourValue) > 100 {
						ourValue = ourValue[:100] + "..."
					}
					t.Errorf("difference in %q\nexpect: %s\n   got: %s", name, theirValue, ourValue)
				}
			}
		})
	}
}

func dumpOurImplementation(r io.ReaderAt) (files map[string]string, err error) {
	fsys := New(r)
	files = make(map[string]string)
	err = fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		fi, err := d.Info()
		if err != nil {
			panic(err)
		}

		switch d.Type() {
		case fs.ModeDir:
			files[name] = "directory"
		case fs.ModeSymlink:
			targ, _ := fsys.(interface{ ReadLink(string) (string, error) }).ReadLink(name)
			files[name] = "link=" + targ
		case 0:
			files[name] = "file=" + strconv.Itoa(int(fi.Size()))
			f, err := fsys.Open(name)
			if err != nil {
				files[name] += "=unopenable(" + comparableErrorString(err) + ")"
				return nil
			}
			defer f.Close()
			data, _ := io.ReadAll(io.LimitReader(f, 10000000))
			files[name] += "=" + hex.EncodeToString(data)
		default:
			panic("bad file type!")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func dumpStdlibImplementation(r io.Reader) (files map[string]string, err error) {
	tar := gotar.NewReader(r)
	files = make(map[string]string)
	for {
		var hdr *gotar.Header
		hdr, err := tar.Next()
		switch err {
		case gotar.ErrInsecurePath:
			err = nil // we don't mind
		case io.EOF:
			return files, nil // done
		case nil:
			// ok
		default:
			return nil, err // uh oh
		}

		// Currently our implementation is UTF8-only
		cleanPath := strings.Trim(hdr.Name, "/")

		switch hdr.Typeflag {
		case gotar.TypeReg, gotar.TypeGNUSparse:
			files[cleanPath] = "file=" + strconv.Itoa(int(hdr.Size))
			if !fs.ValidPath(cleanPath) {
				files[cleanPath] += "=unopenable(" + comparableErrorString(fs.ErrInvalid) + ")"
				continue
			}
			data, _ := io.ReadAll(io.LimitReader(tar, 10000000))
			files[cleanPath] += "=" + hex.EncodeToString(data)
		case gotar.TypeDir:
			files[cleanPath] = "directory"
		case gotar.TypeSymlink:
			l, isAbs := strings.CutPrefix(hdr.Linkname, "/")
			if !isAbs {
				l = path.Join(cleanPath, "..", hdr.Linkname)
			}
			files[cleanPath] = "link=" + l
		}
	}
}

func comparableErrorString(err error) string {
	s := fmt.Sprint(err)
	_, snipped, ok := strings.Cut(s, ": ")
	if ok {
		return snipped
	}
	return s
}
