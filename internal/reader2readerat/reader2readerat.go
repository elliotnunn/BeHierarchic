package reader2readerat

import (
	"io"
	"io/fs"
)

// If the io.Reader is an io.ReadCloser then it will be closed when I am closed
func NewFromReader(f func() (io.Reader, error)) *Reader {
	s := initCommon()
	s.open = func() error {
		r, err := f()
		s.r = r
		return err
	}
	s.close = func() {
		if closer, ok := s.r.(io.Closer); ok {
			closer.Close()
		}
		s.r = nil
	}
	return s
}

func NewFromReadSeeker(r io.ReadSeeker) *Reader {
	s := initCommon()
	s.open = func() error {
		_, err := r.Seek(0, io.SeekStart)
		s.r = r
		return err
	}
	s.close = func() {
		s.r = nil
	}
	return s
}

func initCommon() *Reader {
	r := &Reader{
		req: make(chan request),
		rep: make(chan reply),
	}
	go r.goro()
	return r
}

type Reader struct {
	r     io.Reader
	open  func() error
	close func()
	req   chan request
	rep   chan reply
}

type request struct {
	buf    []byte
	offset int64
}

type reply struct {
	n   int
	err error
}

func (r *Reader) ReadAt(buf []byte, off int64) (n int, err error) {
	func() {
		r := recover()
		if r != nil {
			n = 0
			err = fs.ErrClosed
		}
	}()
	r.req <- request{buf, off}
	rep := <-r.rep
	return rep.n, rep.err
}

func (r *Reader) Close() error {
	close(r.req)
	return nil
}

func (r *Reader) goro() {
	progress := int64(0)
	for cmd := range r.req {
		if progress > cmd.offset || r.r == nil {
			progress = 0
			r.close()
			if err := r.open(); err != nil {
				r.rep <- reply{0, err}
				r.close()
				progress = 0
				continue // make no further attempt to try to satisfy this one
			}
		}

		n64, err := io.CopyN(io.Discard, r.r, cmd.offset-progress)
		progress += n64
		if err != nil {
			r.rep <- reply{0, err}
			continue
		}

		n, err := io.ReadFull(r.r, cmd.buf)
		progress += int64(n)
		r.rep <- reply{n, err}
	}
	r.close()
}
