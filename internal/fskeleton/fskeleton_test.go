// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
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
	fsys.Mkdir("dirThatExists", 0, 0, time.Time{})
	mustNotBlock(t, func() {
		_, err := fsys.Open("dirThatExists")
		expectErr(t, nil, err)
	})
}
func TestInvalidPath(t *testing.T) {
	fsys := New()
	expectErr(t, fs.ErrInvalid, fsys.Symlink("..", 0, "dangling", 0, time.Time{}))
	expectErr(t, fs.ErrInvalid, fsys.Symlink("symlink", 0, "..", 0, time.Time{}))
	expectErr(t, fs.ErrInvalid, fsys.Symlink("", 0, "hmm", 0, time.Time{}))
	_, err := fsys.Open("")
	expectErr(t, fs.ErrInvalid, err)
}
func TestTooLate(t *testing.T) {
	fsys := New()
	fsys.NoMore()
	expectErr(t, fs.ErrClosed, fsys.Mkdir("a", 0, 0, time.Time{}))
	expectErr(t, fs.ErrClosed, fsys.CreateReader("b", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, fs.ErrClosed, fsys.Symlink("c", 0, ".", 0, time.Time{}))
}
func TestIncompleteDir(t *testing.T) {
	fsys := New()
	fsys.CreateReader("a/b/c", 0, emptyFile, 0, 0, time.Time{})
	fsys.CreateReader("a/bb", 0, emptyFile, 0, 0, time.Time{})
	expectStr(t, "...", listDir(fsys, "a/b"))
	expectStr(t, "...", listDir(fsys, "a"))
	fsys.NoMore()
	expectStr(t, "c", listDir(fsys, "a/b"))
	expectStr(t, "b,bb", listDir(fsys, "a"))
}
func TestReRead(t *testing.T) {
	fsys := New()
	fsys.CreateReader("a/b", 0, emptyFile, 0, 0, time.Time{})
	fsys.CreateReader("a/c", 0, emptyFile, 0, 0, time.Time{})
	fsys.CreateReader("a/d", 0, emptyFile, 0, 0, time.Time{})
	fsys.CreateReader("a/e", 0, emptyFile, 0, 0, time.Time{})
	fsys.NoMore()
	f, _ := fsys.Open("a")
	d := f.(fs.ReadDirFile)
	s, err := d.ReadDir(1)
	if len(s) != 1 || err != nil {
		t.Error("tried to read 1, got", s, err)
	}
	s, err = d.ReadDir(2)
	if len(s) != 2 || err != nil {
		t.Error("tried to read 2, got", s, err)
	}
	s, err = d.ReadDir(2)
	if len(s) != 1 || err != io.EOF {
		t.Error("tried to read 1 at end, got", s, err)
	}
}
func TestIncompleteRootInfo(t *testing.T) {
	fsys := New()
	stat, err := fs.Stat(fsys, ".")
	expectErr(t, nil, err)
	mustBlock(t, func() { stat.ModTime() })
	fsys.CreateReader(".", 0, emptyFile, 0, 0, time.Time{})
	mustBlock(t, func() { stat.ModTime() })
	fsys.Mkdir(".", 0, 0, time.Time{})
	mustNotBlock(t, func() { stat.ModTime() })
}
func TestRootInfoWithNoMoreChildren(t *testing.T) {
	fsys := New()
	stat, err := fs.Stat(fsys, ".")
	expectErr(t, nil, err)
	mustBlock(t, func() { stat.ModTime() })
	fsys.NoMore()
	mustNotBlock(t, func() {})
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
	err := fsys.CreateReader("d/f", 0, emptyFile, 0, 0, time.Time{})
	expectErr(t, nil, err)
	fstat, err := fs.Stat(fsys, "d/f")
	expectErr(t, nil, err)
	dstat, err := fs.Stat(fsys, "d")
	expectErr(t, nil, err)
	mustNotBlock(t, func() { fstat.ModTime() })
	mustBlock(t, func() { dstat.ModTime() })
	err = fsys.Mkdir("d", 0, 0, time.Time{})
	expectErr(t, nil, err)
	mustNotBlock(t, func() { dstat.ModTime() })
}
func TestDirCreation(t *testing.T) {
	fsys := New()
	expectErr(t, nil, fsys.CreateReader("notadir", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Symlink("alsonotadir", 0, "dangling", 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.CreateReader("notadir", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.CreateReader("alsonotadir", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.CreateReader("notadir/yet/it/contains/something", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.CreateReader("alsonotadir/yet/it/contains/something", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Mkdir("implicit/explicit", 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.Mkdir("implicit/explicit", 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Mkdir("implicit", 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.Mkdir("implicit", 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Mkdir(".", 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.Mkdir(".", 0, 0, time.Time{}))
}
func TestFullyNonblocking(t *testing.T) {
	fsys := New()
	expectErr(t, nil, fsys.CreateReader("imp/exp", 0, emptyFile, 0, 0, time.Time{}))
	fsys.NoMore()
	fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		mustNotBlock(t, func() { s, _ := fsys.Stat(name); s.Sys() })
		mustNotBlock(t, func() { fs.ReadDir(fsys, name) })
		return nil
	})
}
func TestSymlink(t *testing.T) {
	fsys := New()
	expectErr(t, nil, fsys.Symlink("symlink1", 0, "file1", 0, time.Time{})) // dangling symlink
	expectErr(t, nil, fsys.Symlink("symlink2", 0, "file2", 0, time.Time{}))
	expectErr(t, nil, fsys.Symlink("symlink3", 0, "dir3", 0, time.Time{}))
	expectErr(t, nil, fsys.Symlink("symlink4", 0, "symlink3/file5", 0, time.Time{}))
	expectErr(t, nil, fsys.Symlink("symlink6", 0, "symlink6", 0, time.Time{})) // circular
	expectErr(t, nil, fsys.Mkdir("dir3", 0, 0, time.Time{}))
	expectErr(t, nil, fsys.CreateReader("file2", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.CreateReader("dir3/file5", 0, emptyFile, 0, 0, time.Time{}))

	mustBlock(t, func() { fsys.Open("symlink1") })
	fsys.NoMore()
	mustNotBlock(t, func() { fsys.Open("symlink1") })

	s, err := fsys.Lstat("symlink1") // dangling symlink
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.ModeSymlink.String())
	_, err = fsys.Stat("symlink1")
	expectErr(t, fs.ErrNotExist, err)
	target, err := fsys.ReadLink("symlink1")
	expectErr(t, err, nil)
	expectStr(t, "file1", target)

	s, err = fsys.Lstat("symlink2") // good symlink
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.ModeSymlink.String())
	_, err = fsys.Open("symlink2")
	expectErr(t, nil, err)
	s, err = fsys.Stat("symlink2")
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.FileMode(0).String())
	target, err = fsys.ReadLink("symlink2")
	expectErr(t, err, nil)
	expectStr(t, "file2", target)

	expectStr(t, "file5", listDir(fsys, "symlink3"))

	s, err = fsys.Lstat("symlink4") // symlink through another symlink
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.ModeSymlink.String())
	s, err = fsys.Stat("symlink4")
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.FileMode(0).String())

	s, err = fsys.Lstat("symlink6") // circular symlink
	expectErr(t, nil, err)
	expectStr(t, s.Mode().String(), fs.ModeSymlink.String())
	_, err = fsys.Stat("symlink6")
	expectErr(t, fs.ErrNotExist, err)
	target, err = fsys.ReadLink("symlink6")
	expectErr(t, err, nil)
	expectStr(t, "symlink6", target)
}
func TestTime(t *testing.T) {
	times := []time.Time{
		{},
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.FixedZone("obscura", 120)),
		time.Now(),
		time.Now().In(time.FixedZone("obscura", 120)),
		time.Now().UTC(),
	}
	fsys := New()
	for i, time := range times {
		fsys.CreateError(strconv.Itoa(i), int64(i), io.EOF, 0, 0, time)
	}
	for i, time := range times {
		inf, _ := fs.Stat(fsys, strconv.Itoa(i))
		gottime := inf.ModTime()
		if !time.Equal(gottime) {
			t.Errorf("expected %s, got %s", time, gottime)
		}
	}
}
func TestList(t *testing.T) {
	fsys := New()
	expectStr(t, ".(d)...", listFS(fsys, true))
	expectStr(t, ".(d)", listFS(fsys, false))
	expectErr(t, nil, fsys.CreateReader("ddd/aaa", 0, emptyFile, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Mkdir("bbb", 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Symlink("ccc", 0, "ddd/aaa", 0, time.Time{}))
	expectStr(t, ".(d) ddd(d) ddd/aaa(f) bbb(d) ccc(l)...", listFS(fsys, true))
	expectStr(t, ".(d) ddd(d) ddd/aaa(f) bbb(d) ccc(l)", listFS(fsys, false))
	fsys.NoMore()
	expectStr(t, ".(d) ddd(d) ddd/aaa(f) bbb(d) ccc(l)", listFS(fsys, true))
	expectStr(t, ".(d) ddd(d) ddd/aaa(f) bbb(d) ccc(l)", listFS(fsys, false))
}
func TestRead(t *testing.T) {
	const lips = "lorem ipsum"
	fsys := New()
	readerAtFunc := func() (io.Reader, error) { return strings.NewReader(lips), nil }
	expectErr(t, nil, fsys.CreateReader("Reader", int64(len(lips)), readerAtFunc, 0, 0, time.Time{}))
	readCloserAtFunc := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(lips)), nil }
	expectErr(t, nil, fsys.CreateReadCloser("ReadCloser", int64(len(lips)), readCloserAtFunc, 0, 0, time.Time{}))
	readerAt := strings.NewReader(lips)
	expectErr(t, nil, fsys.CreateReaderAt("ReaderAt", int64(len(lips)), readerAt, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.CreateError("EmptyFile", 0, io.EOF, 0, 0, time.Time{}))
	fsys.NoMore()
	err := fstest.TestFS(fsys, "Reader", "ReadCloser", "ReaderAt", "EmptyFile")
	if err != nil {
		t.Error(err)
	}
}
func TestID(t *testing.T) {
	type IDer interface {
		ID() int64
	}
	fsys := New()
	fsys.Mkdir("implicitDir/realDir", 123, 0, time.Time{})
	fsys.CreateReader("emptyFile", 456, emptyFile, 0, 0, time.Time{})
	fsys.Symlink("link", 789, "implicitDir", 0, time.Time{})
	stat1, _ := fs.Stat(fsys, "implicitDir")
	stat2, _ := fs.Stat(fsys, "implicitDir/realDir")
	stat3, _ := fs.Stat(fsys, "emptyFile")
	stat4, _ := fs.Stat(fsys, "link")
	stat5, _ := fs.Lstat(fsys, "link")
	mustBlock(t, func() { stat1.(IDer).ID() })
	mustNotBlock(t, func() { stat2.(IDer).ID() })
	mustNotBlock(t, func() { stat3.(IDer).ID() })
	mustBlock(t, func() { stat4.(IDer).ID() })
	mustNotBlock(t, func() { stat5.(IDer).ID() })
	expectStr(t, "123 456 789", fmt.Sprint(stat2.(IDer).ID(), stat3.(IDer).ID(), stat5.(IDer).ID()))
}
func TestCreateRoot(t *testing.T) {
	fsys := New()
	expectErr(t, fs.ErrExist, fsys.CreateError(".", 0, nil, 0, 0, time.Time{}))
	expectErr(t, nil, fsys.Mkdir(".", 0, 0, time.Time{}))
	expectErr(t, fs.ErrExist, fsys.Mkdir(".", 0, 0, time.Time{}))
}
func TestSizes(t *testing.T) {
	cc := []int64{math.MinInt64,
		packSzBtm - 1, packSzBtm, packSzBtm + 1,
		-1, 0, 1, 2,
		packSzTop - 1, packSzTop, packSzTop + 1,
		math.MaxInt64,
	}
	fsys := New()
	for _, size := range cc {
		fsys.CreateError(fmt.Sprint(size), 0, io.ErrUnexpectedEOF, size, 0o755, time.Time{})
	}
	fsys.NoMore()
	for _, size := range cc {
		s, _ := fs.Stat(fsys, fmt.Sprint(size))
		if s.Size() != size {
			t.Errorf("created file with size %d, got back %d", size, s.Size())
		}
	}
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

var emptyFile = func() (io.Reader, error) { return strings.NewReader(""), nil }

func expectErr(t *testing.T, want, got error) {
	if !errors.Is(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func expectStr(t *testing.T, want, got string) {
	if want != got {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// appends "..." if the list function blocked
func listDir(fsys *FS, name string) string {
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

func listFS(fsys *FS, waitFull bool) string {
	ch1, ch2 := make(chan string), make(chan fs.FileMode)
	go func() {
		for name, mode := range fsys.Walk(waitFull) {
			ch1 <- name.String()
			ch2 <- mode
		}
		close(ch1)
		close(ch2)
	}()

	var b strings.Builder
	for {
		select {
		case <-time.After(time.Millisecond * 100):
			return b.String() + "..."
		case name, ok := <-ch1:
			if !ok {
				return b.String()
			}
			mode := <-ch2
			kind := ""
			switch mode {
			case 0:
				kind = "f"
			case fs.ModeDir:
				kind = "d"
			case fs.ModeSymlink:
				kind = "l"
			default:
				kind = mode.String()
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%s(%s)", name, kind)
		}
	}
}
