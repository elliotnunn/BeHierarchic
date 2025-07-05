// Dependence on "testify" ineptly broken by Elliot Nunn

package tarfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"
)

func TestFS(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(tfs, "bar", "foo", "dir1", "dir1/dir11", "dir1/dir11/file111", "dir1/file11", "dir1/file12", "dir2", "dir2/dir21", "dir2/dir21/file211", "dir2/dir21/file212")
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenInvalid(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"/foo", "./foo", "foo/", "foo/../foo", "foo//bar"} {
		_, err := tfs.Open(name)
		if !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("when tarfs.Open(%#v)", name)
		}
	}
}

func TestOpenNotExist(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"baz", "qwe", "foo/bar", "file11"} {
		_, err := tfs.Open(name)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("when tarfs.Open(%#v)", name)
		}
	}
}

func TestOpenThenStat(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range []struct {
		path  string
		name  string
		isDir bool
	}{
		{"foo", "foo", false},
		{"bar", "bar", false},
		{"dir1", "dir1", true},
		{"dir1/file11", "file11", false},
		{".", ".", true},
	} {
		f, err := tfs.Open(file.path)
		if err != nil {
			t.Fatalf("when tarfs.Open(%#v)", file.path)
		}

		fi, err := f.Stat()
		if err != nil {
			t.Fatalf("when file{%#v}.Stat()", file.path)
		}

		if file.name != fi.Name() {
			t.Fatalf("file{%#v}.Stat().Name()", file.path)
		}
		if file.isDir != fi.IsDir() {
			t.Fatalf("file{%#v}.Stat().IsDir()", file.path)
		}
	}
}

func TestOpenThenReadAll(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range []struct {
		path    string
		content []byte
	}{
		{"foo", []byte("foo")},
		{"bar", []byte("bar")},
		{"dir1/file11", []byte("file11")},
	} {
		f, err := tfs.Open(file.path)
		if err != nil {
			t.Fatalf("when tarfs.Open(%#v)", file.path)
		}

		content, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("when io.ReadAll(file{%#v})", file.path)
		}

		if string(file.content) != string(content) {
			t.Fatalf("content of %#v", file.path)
		}
	}
}

func TestOpenThenSeekAfterEnd(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	r, err := tfs.Open("foo")
	if err != nil {
		t.Fatalf("when tarfs.Open(foo)")
	}

	rs := r.(io.ReadSeeker)

	abs, err := rs.Seek(10, io.SeekStart)
	if err != nil {
		t.Fatalf("when ReadSeeker.Seek(10, io.SeekStart)")
	}
	if int64(10) != abs {
		t.Fatal("when ReadSeeker.Seek(10, io.SeekStart)")
	}

	b := make([]byte, 0, 1)
	_, err = rs.Read(b)
	if !errors.Is(err, io.EOF) {
		t.Fatal("when ReadSeeker.Read([]byte)")
	}
}

func TestReadDir(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []struct {
		name       string
		entriesLen int
	}{
		{".", 4},
		{"dir1", 3},
		{"dir2/dir21", 2},
	} {
		entries, err := fs.ReadDir(tfs, dir.name)
		if err != nil {
			t.Fatalf("when fs.ReadDir(tfs, %#v)", dir.name)
		}

		if dir.entriesLen != len(entries) {
			t.Fatalf("len(entries) for %#v", dir.name)
		}
	}
}

func TestReadDirNotDir(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"foo", "dir1/file12"} {
		_, err := fs.ReadDir(tfs, name)
		if !errors.Is(err, ErrNotDir) {
			t.Fatalf("when tarfs.ReadDir(tfs, %#v)", name)
		}
	}
}

func TestReadFile(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range []struct {
		path    string
		content string
	}{
		{"bar", "bar"},
		{"dir1/dir11/file111", "file111"},
		{"dir1/file11", "file11"},
		{"dir1/file12", "file12"},
		{"dir2/dir21/file211", "file211"},
		{"dir2/dir21/file212", "file212"},
		{"foo", "foo"},
	} {
		b, err := fs.ReadFile(tfs, file.path)
		if err != nil {
			t.Fatalf("when fs.ReadFile(tfs, %#v)", file.path)
		}

		if file.content != string(b) {
			t.Fatalf("in %#v", file.path)
		}
	}
}

func TestStat(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range []struct {
		path  string
		name  string
		isDir bool
	}{
		{"dir1/dir11/file111", "file111", false},
		{"foo", "foo", false},
		{"dir2/dir21", "dir21", true},
		{".", ".", true},
	} {
		fi, err := fs.Stat(tfs, file.path)
		if err != nil {
			t.Fatalf("when fs.Stat(tfs, %#v)", file.path)
		}

		if file.name != fi.Name() {
			t.Fatalf("FileInfo{%#v}.Name()", file.path)
		}

		if file.isDir != fi.IsDir() {
			t.Fatalf("FileInfo{%#v}.IsDir()", file.path)
		}
	}
}

func TestGlob(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for pattern, expected := range map[string][]string{
		"*/*2*":   {"dir1/file12", "dir2/dir21"},
		"*":       {"bar", "dir1", "dir2", "foo", "."},
		"*/*/*":   {"dir1/dir11/file111", "dir2/dir21/file211", "dir2/dir21/file212"},
		"*/*/*/*": nil,
	} {
		actual, err := fs.Glob(tfs, pattern)
		if err != nil {
			t.Fatalf("when fs.Glob(tfs, %#v)", pattern)
		}

		slices.Sort(expected)
		slices.Sort(actual)
		if !slices.Equal(expected, actual) {
			t.Fatalf("matches for pattern %#v", pattern)
		}
	}
}

func TestSubThenReadDir(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []struct {
		name       string
		entriesLen int
	}{
		{".", 4},
		{"dir1", 3},
		{"dir2/dir21", 2},
	} {
		subfs, err := fs.Sub(tfs, dir.name)
		if err != nil {
			t.Fatalf("when fs.Sub(tfs, %#v)", dir.name)
		}

		entries, err := fs.ReadDir(subfs, ".")
		if err != nil {
			t.Fatalf("when fs.ReadDir(subfs, %#v)", dir.name)
		}

		if dir.entriesLen != len(entries) {
			t.Fatalf("len(entries) for %#v", dir.name)
		}
	}
}

func TestSubThenReadFile(t *testing.T) {
	f, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	name := "dir2"

	subfs, err := fs.Sub(tfs, name)
	if err != nil {
		t.Fatalf("when fs.Sub(tfs, %#v)", name)
	}

	name = "dir21/file211"
	content := "file211"

	b, err := fs.ReadFile(subfs, name)
	if err != nil {
		t.Fatalf("when fs.ReadFile(subfs, %#v)", name)
	}

	if content != string(b) {
		t.Fatalf("in %#v", name)
	}
}

func TestReadOnDir(t *testing.T) {
	tf, err := os.Open("test.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer tf.Close()

	tfs, err := New(tf)
	if err != nil {
		t.Fatal(err)
	}

	var dirs = []string{"dir1", "dir2/dir21", "."}

	for _, name := range dirs {
		f, err := tfs.Open(name)
		if err != nil {
			t.Fatalf("when fs.ReadFile(subfs, %#v)", name)
		}

		_, err = f.Read(make([]byte, 1))
		if !errors.Is(err, ErrDir) {
			t.Fatalf("when file{%#v}.Read()", name)
		}

		_, err = fs.ReadFile(tfs, name)
		if !errors.Is(err, ErrDir) {
			t.Fatalf("fs.ReadFile(tfs, %#v)", name)
		}
	}
}

func TestWithDotDirInArchive(t *testing.T) {
	f, err := os.Open("test-with-dot-dir.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(tfs, "bar", "foo", "dir1", "dir1/dir11", "dir1/dir11/file111", "dir1/file11", "dir1/file12", "dir2", "dir2/dir21", "dir2/dir21/file211", "dir2/dir21/file212")
	if err != nil {
		t.Fatal(err)
	}
}

func TestWithNoDirEntriesInArchive(t *testing.T) {
	f, err := os.Open("test-no-directory-entries.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(tfs, "bar", "foo", "dir1", "dir1/dir11", "dir1/dir11/file111", "dir1/file11", "dir1/file12", "dir2", "dir2/dir21", "dir2/dir21/file211", "dir2/dir21/file212")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSparse(t *testing.T) {
	f, err := os.Open("test-sparse.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(tfs, "file1", "file2")
	if err != nil {
		t.Fatal(err)
	}

	file1Actual, err := fs.ReadFile(tfs, "file1")
	if err != nil {
		t.Fatalf("fs.ReadFile(tfs, \"file1\")")
	}
	file1Expected := make([]byte, 1000000)
	copy(file1Expected, []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	copy(file1Expected[999990:], []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	if string(file1Expected) != string(file1Actual) {
		t.Fatal("fs.ReadFile(tfs, \"file1\")")
	}

	file2Actual, err := fs.ReadFile(tfs, "file2")
	if err != nil {
		t.Fatalf("fs.ReadFile(tfs, \"file2\")")
	}
	if "file2" != string(file2Actual) {
		t.Fatal("fs.ReadFile(tfs, \"file2\")")
	}
}

func TestIgnoreGlobalHeader(t *testing.T) {
	// This file was created by initializing a git repository,
	// committing a few files, and running: `git archive HEAD`
	f, err := os.Open("test-with-global-header.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tfs, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(tfs, "bar", "dir1", "dir1/file11")
	if err != nil {
		t.Fatal(err)
	}
}
