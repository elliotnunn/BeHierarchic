// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package resourcefork

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"io/fs"
	"testing"
	"testing/fstest"
)

//go:embed testbinaries/large.rsrc
var large []byte

func TestLarge(t *testing.T) {
	fsys, err := New(bytes.NewReader(large))
	if err != nil {
		t.Fatal(err)
	}
	err = fstest.TestFS(fsys, "0b  /-32768", "0b  /32767", "99b /-32768", "99b /32767")
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
	data, err := fs.ReadFile(fsys, "99b /-32768")
	if len(data) != 99 || len(bytes.ReplaceAll(data, []byte{0xee}, nil)) != 0 {
		t.Errorf("expected resource of type '99b ' to contain 0xee x 99, got %s", hex.EncodeToString(data))
	}
}

//go:embed testbinaries/empty.rsrc
var empty []byte

func TestEmpty(t *testing.T) {
	fsys, err := New(bytes.NewReader(empty))
	if err != nil {
		t.Fatal(err)
	}
	err = fstest.TestFS(fsys)
	if err != nil {
		t.Error(err)
	}
}

//go:embed testbinaries/named.rsrc
var named []byte

func TestNamed(t *testing.T) {
	fsys, err := New(bytes.NewReader(named))
	if err != nil {
		t.Fatal(err)
	}
	err = fstest.TestFS(fsys, "blan/128", "long/128")
	if err != nil {
		t.Error(err)
	}

	to, err := fs.ReadLink(fsys, "blan/named/_")
	if err != nil || to != "blan/128" {
		t.Error(err)
	}
}
