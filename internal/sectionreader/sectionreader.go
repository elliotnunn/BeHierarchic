// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package sectionreader

import (
	"io"
	"math"
)

func Section(r io.ReaderAt, off int64, n int64) *ReaderAt {
	for {
		t, ok := r.(*io.SectionReader)
		if !ok {
			break
		}
		outer, outerOff, outerN := t.Outer()
		if off+n > outerN {
			break
		}
		r, off = outer, off+outerOff
	}

	return &ReaderAt{r, off, n}
}

type ReaderAt struct {
	r      io.ReaderAt
	off, n int64
}

func (r *ReaderAt) Outer() (io.ReaderAt, int64, int64) { return r.r, r.off, r.n }

func (s *ReaderAt) Size() int64 { return s.n }

func (s *ReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if s.n < 0 || s.off < 0 || off < 0 || s.off+off < 0 || off >= s.n {
		return 0, io.EOF
	}

	ourlimit := s.off + s.n
	if ourlimit < s.off { // integer overflow
		ourlimit = math.MaxInt64
	}

	off += s.off
	if max := ourlimit - off; int64(len(p)) > max {
		p = p[:max]
		n, err = s.r.ReadAt(p, off)
		if err == nil {
			err = io.EOF
		}
		return n, err
	}
	return s.r.ReadAt(p, off)
}
