package appledouble

import (
	"io"
	"math"
	"testing/iotest"
)

type reader struct {
	ad     []byte
	zero   int
	opener func() io.Reader
	fork   io.Reader
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
			r.fork = r.opener()
			if r.fork == nil {
				r.fork = iotest.ErrReader(io.EOF)
			}
		}
		return r.fork.Read(p)
	}
}

func (r *reader) Close() error {
	if closer, ok := r.fork.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type readerAt struct {
	ad   []byte
	fork io.ReaderAt
}

func (r *readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	so, do, ncopy := intersect(0, int64(len(r.ad)), off, off+int64(len(p)))
	if ncopy > 0 {
		n += copy(p[do:], r.ad[so:])
	}

	so, do, nread := intersect(int64(len(r.ad)), math.MaxInt64, off, off+int64(len(p)))
	if nread > 0 {
		forkread, forkerr := r.fork.ReadAt(p[do:], so)
		n += forkread
		err = forkerr
	}
	return
}

// Overlap two half-open intervals
func intersect(sa, sz, da, dz int64) (so, do, n int64) {
	if sa >= dz || da >= sz {
		return
	}
	// now we are sure that the ranges overlap
	if sa < da {
		so = da - sa
	} else {
		do = sa - da
	}
	n = min(sz-(sa+so), dz-(da+do))
	return
}
