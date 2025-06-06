package reader2readerat

import (
	"fmt"
	"io"
	"sync"

	"github.com/dgraph-io/ristretto"
)

type ReaderAt struct {
	r     io.Reader
	uniq  string
	open  func() error
	close func()
	l     sync.Mutex
	seek  int64
}

// If the io.Reader is an io.ReadCloser then it will be closed when I am closed
func NewFromReader(uniq string, f func() (io.Reader, error)) *ReaderAt {
	r := initCommon(uniq)
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

func NewFromReadSeeker(uniq string, from io.ReadSeeker) *ReaderAt {
	r := initCommon(uniq)
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

func initCommon(uniq string) *ReaderAt {
	r := &ReaderAt{
		uniq: uniq,
	}
	return r
}

func (r *ReaderAt) ReadAt(buf []byte, off int64) (n int, reterr error) {
	doCacheWait := false
	for base := off / blocksize * blocksize; base < off+int64(len(buf)); base += blocksize {
		var block []byte

		key := fmt.Sprintf("%s@%#x", r.uniq, base)
		if b, ok := cache.Get(key); ok { // easy path
			block = b.([]byte)
			if len(block) < blocksize {
				reterr = io.EOF
			}
		} else {
			block = make([]byte, blocksize)
			r.l.Lock()
			if r.seek > base || r.r == nil {
				r.close()
				if err := r.open(); err != nil {
					return n, err
				}
			}

			for {
				block = block[:blocksize]
				bn, berr := io.ReadFull(r.r, block)
				block = block[:bn]
				r.seek += int64(bn)
				cache.Set(key, block, int64(len(block)))
				doCacheWait = true
				reterr = berr
				if r.seek-int64(bn) == base {
					break
				}
			}
			r.l.Unlock()
		}

		blockskip := max(0, off-base)
		if blockskip > int64(len(block)) {
			reterr = io.EOF
			break
		}
		n += copy(buf[n:], block[blockskip:])
		if reterr != nil {
			break
		}
	}
	if doCacheWait {
		cache.Wait()
	}
	return n, reterr
}

func (r *ReaderAt) Close() error {
	r.close()
	return nil
}

var cache *ristretto.Cache

const (
	blocksize = 4096
	maxcache  = 1 << 30 // gigabyte
)

func init() {
	c, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: maxcache / blocksize * 16,
		MaxCost:     maxcache,
		BufferItems: 64,
	})
	if err != nil {
		panic(err)
	}
	cache = c
}
