package inithint

import "io"

const special = 0xbd

func IsHint(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	for _, c := range p {
		if c != special {
			return false
		}
	}
	return true
}

func ReadAt(r io.ReaderAt, p []byte, off int64) (n int, err error) {
	for i := range p {
		p[i] = special
	}
	return r.ReadAt(p, off)
}

func NewReaderAt(r io.ReaderAt) *ReaderAt {
	return &ReaderAt{r: r}
}

type ReaderAt struct {
	r       io.ReaderAt
	disable bool
}

func (ra *ReaderAt) Disable() {
	ra.disable = true
}

func (ra *ReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if !ra.disable {
		for i := range p {
			p[i] = special
		}
	}
	return ra.r.ReadAt(p, off)
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

type Reader struct {
	r       io.Reader
	disable bool
}

func (ra *Reader) Disable() {
	ra.disable = true
}

func (ra *Reader) Read(p []byte) (n int, err error) {
	if !ra.disable {
		for i := range p {
			p[i] = special
		}
	}
	return ra.r.Read(p)
}
