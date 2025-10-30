package main

import (
	"encoding/hex"
	"fmt"
	"iter"
	"slices"
	"strings"
)

// 32 byte header, similar properties to PNG
// if bumping the version, put it in the last byte
const brMagic = "\x89BeHierarchicCache\x50\x4e\x47\x0d\x0a\xa1\x0a\x00\x00\x00\x00\x00\x00\x00"

// then the rest of the format is just a Go gob

// 89	Has the high bit set to detect transmission systems that do not support 8-bit data and to reduce the chance that a text file is mistakenly interpreted as a PNG, or vice versa.
// 504E47	In ASCII, the letters PNG, allowing a person to identify the format easily if it is viewed in a text editor.
// 0D 0A	A DOS-style line ending (CRLF) to detect DOS-Unix line ending conversion of the data.
// 1A	A byte that stops display of the file under DOS when the command type has been used—the end-of-file character.
// 0A

type byteRangeList []byteRange
type byteRange struct {
	Off int64
	Buf []byte
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
		if a.Off+int64(len(a.Buf)) < b {
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
	got, want := (*l)[i], byteRange{off, p}
	if got.end() < want.end() {
		return false
	}
	copy(want.Buf, got.Buf[want.Off-got.Off:])
	return true
}

func (l *byteRangeList) Set(p []byte, off int64) {
	i, hit := slices.BinarySearchFunc(*l, off, func(a byteRange, b int64) int {
		if a.Off+int64(len(a.Buf)) < b {
			return -1
		} else if a.Off > b {
			return 1
		} else {
			return 0
		}
	})

	r := byteRange{off, p}
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
