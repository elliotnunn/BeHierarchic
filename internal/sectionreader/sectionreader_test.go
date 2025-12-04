// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package sectionreader

import (
	"io"
	"math"
	"strings"
	"testing"
)

type outer interface {
	Outer() (io.ReaderAt, int64, int64)
}

func TestBasic(t *testing.T) {
	var abcd io.ReaderAt = strings.NewReader("abcd")
	var r io.ReaderAt

	r = Section(abcd, 0, 4)
	expectRead(t, r, 0, 4, "abcd")
	expectRead(t, r, 0, 5, "abcd EOF")
	expectRead(t, r, 4, 1, " EOF")
	expectRead(t, r, math.MaxInt64, 1, " EOF")

	r = Section(abcd, 1, 4)
	expectRead(t, r, 0, 4, "bcd EOF")
	expectRead(t, r, 0, 2, "bc")
}

func TestOverflow(t *testing.T) {
	var abcd io.ReaderAt = strings.NewReader("abcd")
	var r io.ReaderAt

	r = Section(abcd, 0, math.MaxInt64)
	expectRead(t, r, 0, 4, "abcd")
	expectRead(t, r, 0, 5, "abcd EOF")
	expectRead(t, r, math.MinInt64+2, 1, " EOF")

	r = Section(abcd, 10, math.MaxInt64)
	expectRead(t, r, math.MaxInt64, 1, " EOF")

	r = Section(abcd, math.MaxInt64, math.MaxInt64)
	expectRead(t, r, 0, 1, " EOF")
}

func TestUnwrap(t *testing.T) {
	var abcd io.ReaderAt = strings.NewReader("abcd")
	var r io.ReaderAt

	r = Section(io.NewSectionReader(abcd, 0, 3), 1, 2)
	expectRead(t, r, 0, 4, "bc EOF")
	expectRead(t, r, 0, 5, "bc EOF")
	unwrap, _, _ := r.(outer).Outer()
	if unwrap != abcd {
		t.Errorf("expected Section(SectionReader(r)) to expose the original r through Outer(), got %T", unwrap)
	}

	r = Section(io.NewSectionReader(abcd, 0, 3), 1, 5)
	unwrap, _, _ = r.(outer).Outer()
	if _, ok := unwrap.(*io.SectionReader); !ok {
		t.Errorf("expected Section(SectionReader(r)) to expose the SectionReader through Outer(), got %T", unwrap)
	}
}

func expectRead(t *testing.T, r io.ReaderAt, off int64, n int, expect string) {
	buf := make([]byte, n)
	gotn, err := r.ReadAt(buf, off)
	gots := string(buf[:gotn])
	if err != nil {
		gots += " " + err.Error()
	}
	if gots != expect {
		t.Errorf("ReadAt(%d bytes at offset %d) -> expected %q got %q", n, off, expect, gots)
	}
}
