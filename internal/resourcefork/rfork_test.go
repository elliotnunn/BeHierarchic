// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package resourcefork

import (
	"bytes"
	_ "embed"
	"io/fs"
	"testing"
	"testing/fstest"
)

//go:embed testbinaries/large.rsrc
var large []byte

func TestLarge(t *testing.T) {
	fsys := &FS{AppleDouble: bytes.NewReader(large)}
	err := fstest.TestFS(fsys, "0b  /-32768", "0b  /32767", "99b /-32768", "99b /32767")
	if err != nil {
		t.Error(err)
	}

	s, err := fs.Stat(fsys, "0b  /-32768")
	if err != nil {
		t.Error(err)
	}
	if s.Size() != 0 {
		t.Errorf("expected resource of type '0b  ' to be 0 bytes, got %d", s.Size())
	}

	s, err = fs.Stat(fsys, "99b /-32768")
	if err != nil {
		t.Error(err)
	}
	if s.Size() != 99 {
		t.Errorf("expected resource of type '99b ' to be 99 bytes, got %d", s.Size())
	}
}

//go:embed testbinaries/empty.rsrc
var empty []byte

func TestEmpty(t *testing.T) {
	fsys := &FS{AppleDouble: bytes.NewReader(empty)}
	err := fstest.TestFS(fsys)
	if err != nil {
		t.Error(err)
	}
}
