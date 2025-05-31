package reader2readerat

import (
	"io"
	"io/fs"
)

type ReaderAt struct {
	r     io.Reader
	open  func() error
	close func()
	req   chan request
	rep   chan reply
}

// If the io.Reader is an io.ReadCloser then it will be closed when I am closed
func NewFromReader(f func() (io.Reader, error)) *ReaderAt {
	r := initCommon()
	r.open = func() error {
		from, err := f()
		r.r = from
		return err
	}
	r.close = func() {
		if closer, ok := r.r.(io.Closer); ok {
			closer.Close()
		}
		r.r = nil
	}
	return r
}

func NewFromReadSeeker(from io.ReadSeeker) *ReaderAt {
	r := initCommon()
	r.open = func() error {
		_, err := from.Seek(0, io.SeekStart)
		r.r = from
		return err
	}
	r.close = func() {
		r.r = nil
	}
	return r
}

func initCommon() *ReaderAt {
	r := &ReaderAt{
		req: make(chan request),
		rep: make(chan reply),
	}
	go r.goro()
	return r
}

type request struct {
	buf    []byte
	offset int64
}

type reply struct {
	n   int
	err error
}

func (r *ReaderAt) ReadAt(buf []byte, off int64) (n int, err error) {
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

func (r *ReaderAt) Close() error {
	close(r.req)
	return nil
}

func (r *ReaderAt) goro() {
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
