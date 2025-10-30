package main

import (
	"encoding/hex"
	"fmt"
	"iter"
	"slices"
	"strings"
)

type byteRangeList []byteRange
type byteRange struct {
	Buf []byte
	Off int64
}

func (l *byteRangeList) Iterate() iter.Seq2[[]byte, int64] {
	return func(yield func([]byte, int64) bool) {
		for _, r := range *l {
			if !yield(r.Buf, r.Off) {
				return
			}
		}
	}
}

func (l *byteRangeList) Get(p []byte, off int64) bool {
	i, hit := slices.BinarySearchFunc(*l, off, func(a byteRange, b int64) int {
		if a.end() <= b { // need it to be totally contained inside this one
			return -1
		} else if a.Off > b {
			return 1
		} else {
			return 0
		}
	})
	if !hit {
		return false
	}
	got, want := (*l)[i], byteRange{p, off}
	if got.end() < want.end() {
		return false
	}
	n := copy(want.Buf, got.Buf[want.Off-got.Off:])
	if n != len(p) {
		panic("failed sanity check!")
	}
	return true
}

func (l *byteRangeList) Set(p []byte, off int64) {
	i, hit := slices.BinarySearchFunc(*l, off, func(a byteRange, b int64) int {
		if a.end() < b {
			return -1
		} else if a.Off > b {
			return 1
		} else {
			return 0
		}
	})

	r := byteRange{p, off}
	if hit {
		(*l)[i].incorporate(r)
	} else {
		*l = slices.Insert(*l, i, r)
	}

	for i+1 < len(*l) {
		if (*l)[i].incorporate((*l)[i+1]) {
			*l = slices.Delete(*l, i+1, i+2)
		} else {
			break
		}
	}
}

func (l *byteRangeList) String() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, r := range *l {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(r.String())
	}
	b.WriteByte(']')
	return b.String()
}

func (r *byteRange) String() string {
	if len(r.Buf) > 16 {
		return fmt.Sprintf("%d=%s...", r.Off, hex.EncodeToString(r.Buf[:16]))
	} else {
		return fmt.Sprintf("%d=%s", r.Off, hex.EncodeToString(r.Buf))
	}
}

func (r *byteRange) end() int64 { return r.Off + int64(len(r.Buf)) }

func (r *byteRange) incorporate(r2 byteRange) bool {
	if r2.end() < r.Off || r.end() < r2.Off {
		return false // cannot meld together
	}

	// put the leftmost one into r
	if r2.Off < r.Off {
		*r, r2 = r2, *r
	}

	if r2.end() > r.end() {
		r.Buf = append(r.Buf, make([]byte, int(r2.end()-r.end()))...)
	}

	copy(r.Buf[r2.Off-r.Off:], r2.Buf)
	return true
}
