package appledouble

import (
	"fmt"
	"io"
	"io/fs"
	"testing/iotest"
)

type reader struct {
	ad     []byte
	zero   int
	opener func() (io.ReadCloser, error)
	fork   io.ReadCloser
}

func (r *reader) Read(p []byte) (n int, err error) {
	switch {
	case len(r.ad) > 0:
		n = copy(p, r.ad)
		r.ad = r.ad[n:]
		return n, nil
	case r.zero > 0:
		n = min(len(p), r.zero)
		r.zero -= n
		clear(p[:n])
		return n, nil
	default:
		if r.fork == nil {
			r.fork, err = r.opener()
			if err != nil {
				r.fork = io.NopCloser(iotest.ErrReader(err))
			}
		}
		n, err = r.fork.Read(p)
		if err == io.EOF {
			fmt.Println("EOF after a read of", len(p), n)
		}
		return
	}
}

func (r *reader) Close() error {
	if r.fork != nil {
		return r.fork.Close()
	}
	return nil
}

type readerAt struct {
	ad   []byte
	fork io.ReaderAt
}

func (r *readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if off < int64(len(r.ad)) {
		n = copy(p, r.ad[int(off):])
	}
	if n == len(p) {
		return n, nil
	}
	askoff := max(0, off-int64(len(r.ad)))
	fmt.Printf("request for %d bytes @ %d -> %d bytes at %d\n", len(p), off, len(p[n:]), askoff)
	fn, err := r.fork.ReadAt(p[n:], askoff)
	n += fn
	return n, err
}
