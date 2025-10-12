// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"fmt"
	"io"
	"io/fs"
	"testing"
	"time"
)

func TestBlockedOpen(t *testing.T) {
	fsys := New()
	mustBlock(t, func() { fsys.Open("fileThatDoesntExist") })
}
func TestBlockedStat(t *testing.T) {
	fsys := New()
	mustBlock(t, func() { fsys.Stat("fileThatDoesntExist") })
}
func TestBlockedReadLink(t *testing.T) {
	fsys := New()
	mustBlock(t, func() { fsys.ReadLink("fileThatDoesntExist") })
}
func TestBlockedLstat(t *testing.T) {
	fsys := New()
	mustBlock(t, func() { fsys.Lstat("fileThatDoesntExist") })
}
func TestOpenDir(t *testing.T) {
	fsys := New()
	fsys.CreateDir("dirThatExists", 0, time.Time{}, nil)
	mustNotBlock(t, func() {
		_, err := fsys.Open("dirThatExists")
		expectErr(t, nil, err)
	})
}
func TestIncompleteDir(t *testing.T) {
	fsys := New()
	fsys.CreateFile("a/b/c", emptyFile, 0, 0, time.Time{}, nil)
	expectStr(t, "c...", listDir(fsys, "a/b"))
	fsys.NoMoreChildren("a/b")
	expectStr(t, "c", listDir(fsys, "a/b"))
	expectStr(t, "b...", listDir(fsys, "a"))
	fsys.NoMore()
	expectStr(t, "b", listDir(fsys, "a"))
}
func TestIncompleteRootInfo(t *testing.T) {
	fsys := New()
	stat, err := fs.Stat(fsys, ".")
	expectErr(t, nil, err)
	mustBlock(t, func() { stat.ModTime() })
	fsys.CreateFile(".", emptyFile, 0, 0, time.Time{}, nil)
	mustBlock(t, func() { stat.ModTime() })
	fsys.CreateDir(".", 0, time.Time{}, nil)
	mustNotBlock(t, func() { stat.ModTime() })
}
func TestRootInfoWithNoMoreChildren(t *testing.T) {
	fsys := New()
	stat, err := fs.Stat(fsys, ".")
	expectErr(t, nil, err)
	mustBlock(t, func() { stat.ModTime() })
	fsys.NoMoreChildren(".")
	mustBlock(t, func() { stat.ModTime() })
	fsys.NoMoreChildren("..")
	mustNotBlock(t, func() { stat.ModTime() })
}
func TestRootInfoWithNoMore(t *testing.T) {
	fsys := New()
	stat, err := fs.Stat(fsys, ".")
	expectErr(t, nil, err)
	mustBlock(t, func() { stat.ModTime() })
	fsys.NoMore()
	mustNotBlock(t, func() { stat.ModTime() })
}
func TestIncompleteDirInfo(t *testing.T) {
	fsys := New()
	err := fsys.CreateFile("d/f", emptyFile, 0, 0, time.Time{}, nil)
	expectErr(t, nil, err)
	fstat, err := fs.Stat(fsys, "d/f")
	expectErr(t, nil, err)
	dstat, err := fs.Stat(fsys, "d")
	expectErr(t, nil, err)
	mustNotBlock(t, func() { fstat.ModTime() })
	mustBlock(t, func() { dstat.ModTime() })
	err = fsys.CreateDir("d", 0, time.Time{}, nil)
	expectErr(t, nil, err)
	mustNotBlock(t, func() { dstat.ModTime() })
}
func TestDirCreation(t *testing.T) {
	fsys := New()
	expectErr(t, nil, fsys.CreateDir("implicit/explicit", 0, time.Time{}, nil))
	expectErr(t, fs.ErrExist, fsys.CreateDir("implicit/explicit", 0, time.Time{}, nil))
	expectErr(t, nil, fsys.CreateDir("implicit", 0, time.Time{}, nil))
	expectErr(t, fs.ErrExist, fsys.CreateDir("implicit", 0, time.Time{}, nil))
	expectErr(t, nil, fsys.CreateDir(".", 0, time.Time{}, nil))
	expectErr(t, fs.ErrExist, fsys.CreateDir(".", 0, time.Time{}, nil))
}
func TestFullyNonblocking(t *testing.T) {
	fsys := New()
	expectErr(t, nil, fsys.CreateFile("imp/exp", emptyFile, 0, 0, time.Time{}, nil))
	fsys.NoMore()
	fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		mustNotBlock(t, func() { s, _ := fsys.Stat(name); s.Sys() })
		mustNotBlock(t, func() { fs.ReadDir(fsys, name) })
		return nil
	})
}

func mustBlock(t *testing.T, f func()) {
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
		t.Error("should have blocked")
	case <-time.After(time.Millisecond * 100):
	}
}

func mustNotBlock(t *testing.T, f func()) {
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-time.After(time.Millisecond * 100):
		t.Error("should not have blocked")
	case <-done:
	}
}

func emptyFile(f fs.File) (fs.File, error) { return f, nil }

func expectErr(t *testing.T, want, got error) {
	w, e := fmt.Sprint(want), fmt.Sprint(got)
	if w != e {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func expectStr(t *testing.T, want, got string) {
	if want != got {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// appends "..." if the list function blocked
func listDir(fsys *FS.FS, name string) string {
	f, err := fsys.Open(name)
	if err != nil {
		return "!" + err.Error()
	}
	defer f.Close()

	d, ok := f.(fs.ReadDirFile)
	if !ok {
		return "!expected fs.ReadDirFile but got fs.File only"
	}
	ch := make(chan fs.DirEntry)
	ech := make(chan error)
	go func() {
		for {
			l, err := d.ReadDir(1)
			if len(l) > 0 {
				ch <- l[0]
			}
			if err != nil {
				ech <- err
				break
			}
		}
	}()
	s := ""
	for {
		select {
		case <-time.After(time.Millisecond * 100):
			return s + "..."
		case de := <-ch:
			if len(s) > 0 {
				s += ","
			}
			s += de.Name()
		case err := <-ech:
			if err == io.EOF {
				return s
			} else {
				return s + "!" + err.Error()
			}
		}
	}
}
