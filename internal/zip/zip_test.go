// Copyright Elliot Nunn. Portions copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import (
	gozip "archive/zip"
	"bytes"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing"
)

//go:embed testdata
var zips embed.FS

func TestVsStdlib(t *testing.T) {
	zips, _ := fs.Sub(zips, "testdata")
	fs.WalkDir(zips, ".", func(name string, d fs.DirEntry, err error) error {
		if !strings.HasSuffix(name, ".zip") {
			return nil
		}
		t.Run(name, func(t *testing.T) {
			f, _ := zips.Open(name)
			inf, _ := f.Stat()
			defer f.Close()

			fsys, err := New2(f.(io.ReaderAt), f.(io.ReaderAt), inf.Size())
			if err != nil {
				t.Fatal(err)
			}

			stdlib, err := gozip.NewReader(f.(io.ReaderAt), inf.Size())
			if err != nil {
				t.Fatal("the canonical implementation complains", err)
			}

			for _, f := range stdlib.File {
				name := f.Name
				if strings.HasPrefix(name, "__MACOSX/") && strings.HasSuffix(name, "/") {
					continue // junk appledouble directory
				}
				name = strings.TrimPrefix(name, "__MACOSX/")
				name = strings.TrimSuffix(name, "/")

				myinf, err := fs.Lstat(fsys, name)
				if err != nil {
					t.Fatalf("unable to stat %q: %v", f.Name, err)
				}
				if f.Mode()&fs.ModeType != myinf.Mode()&fs.ModeType {
					t.Errorf("mode of %q: expect %s got %s", f.Name, f.Mode(), myinf.Mode())
				}
				if myinf.Mode()&fs.ModeSymlink == 0 && f.UncompressedSize64 != uint64(myinf.Size()) {
					t.Errorf("size of %q: expect %d got %d", f.Name, f.UncompressedSize64, myinf.Size())
				}
				t1 := f.Modified.UTC()
				t2 := myinf.ModTime().UTC()
				tf := "2006-01-02-15:04:05.999999999"
				if !t1.Equal(t2) {
					t.Errorf("mtime of %q: expect %s got %s", f.Name, t1.Format(tf), t2.Format(tf))
				}

				// This is the location of an executable inside a Mac OS X bundle
				if strings.HasSuffix(path.Dir(name), ".app/Contents/MacOS") && !strings.Contains(name, "._") &&
					myinf.Mode()&0o100 == 0 {
					t.Errorf("perms of %q: %s", f.Name, myinf.Mode())
				}

				if f.Mode().IsRegular() {
					theirdata, _ := fs.ReadFile(stdlib, f.Name)

					ourdata, err := fs.ReadFile(fsys, name)
					if err != nil {
						t.Errorf("error reading %q: %v", f.Name, err)
					}
					if !bytes.Equal(theirdata, ourdata) {
						t.Errorf("wrong data reading %q", f.Name)
					}
				}
			}
		})
		return nil
	})
}

func TestPerms(t *testing.T) {
	f, _ := zips.Open("testdata/mine/perms.zip")
	inf, _ := f.Stat()
	defer f.Close()
	fsys, err := New2(f.(io.ReaderAt), f.(io.ReaderAt), inf.Size())
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"noexec", "exec"} {
		inf, err := fs.Stat(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		haveExec := inf.Mode()&0o100 != 0
		wantExec := name == "exec"
		if haveExec != wantExec {
			t.Errorf("%q has perms %s", name, inf.Mode())
		}
	}
}

func TestLinks(t *testing.T) {
	f, _ := zips.Open("testdata/mine/links.zip")
	inf, _ := f.Stat()
	defer f.Close()
	fsys, err := New2(f.(io.ReaderAt), f.(io.ReaderAt), inf.Size())
	if err != nil {
		t.Fatal(err)
	}

	fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if d.Type() != fs.ModeSymlink {
			return nil
		}
		target, err := fs.ReadLink(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		if path.Base(target) != "target"+path.Base(name) {
			t.Errorf("%q: target should not be %q", name, target)
		}
		f, err := fsys.Open(name)
		if err != nil {
			t.Error(err)
		}
		f.Close()
		return nil
	})
}

func TestEOCD(t *testing.T) {
	fs.WalkDir(zips, "testdata/comments", func(name string, d fs.DirEntry, err error) error {
		if !strings.HasSuffix(name, ".zip") {
			return nil
		}
		t.Run(path.Base(name), func(t *testing.T) {
			f, _ := zips.Open(name)
			inf, _ := f.Stat()
			defer f.Close()

			eocd, err := getEOCD(f.(io.ReaderAt), inf.Size())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(eocd, []byte("PK\x05\x06")) {
				t.Fatal("expected EOCD, got", hex.EncodeToString(eocd))
			}

			fullZip, _ := fs.ReadFile(zips, name)
			if !bytes.HasSuffix(fullZip, eocd) {
				t.Fatal("EOCD corrupted")
			}

			// Check that the reader does not read any earlier than the EOCD
			restricted := bytes.NewReader(eocd)
			eocd, err = getEOCD(restricted, restricted.Size())
			if err != nil {
				t.Fatal("read beyond bounds", err)
			}
			if !bytes.HasPrefix(eocd, []byte("PK\x05\x06")) {
				t.Fatal("expected EOCD, got", hex.EncodeToString(eocd))
			}
		})
		return nil
	})
}
