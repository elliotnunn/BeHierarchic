// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package reader2readerat

import (
	"fmt"
	"io"
	"os"
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
	eof   int64
	err   error
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
		r.closeCommon()
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
	r.close = r.closeCommon
	return r
}

func initCommon(uniq string) *ReaderAt {
	r := &ReaderAt{
		uniq: uniq,
	}
	return r
}

func (r *ReaderAt) closeCommon() {
	r.r, r.seek = nil, 0
}

var n = 1

func (r *ReaderAt) getNextBlock() ([]byte, error) {
	buf := make([]byte, blocksize)
	key := r.cacheKey(r.seek)
	n, err := io.ReadFull(r.r, buf)
	r.seek += int64(n)

	if n > blocksize/2 {
		buf = buf[:n]
	} else { // small tail, make a smaller allocation for it
		buf = append(make([]byte, 0, n), buf[:n]...)
	}

	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if err != nil { // the underlying nonseekable reader is exhausted
		r.eof, r.err = r.seek, err
		r.close()
	}
	cache.Set(key, buf, int64(n))
	return buf, err
}

func (r *ReaderAt) ReadAt(buf []byte, off int64) (n int, reterr error) {
	for base := off / blocksize * blocksize; base < off+int64(len(buf)); base += blocksize {
		var block []byte

		k := r.cacheKey(base)
		if b, ok := cache.Get(k); ok { // easy path
			block = b.([]byte)
			if base+int64(len(block)) == r.eof {
				reterr = r.err
			}
		} else {
			r.l.Lock()
			if r.seek > base || r.r == nil {
				r.close()
				if err := r.open(); err != nil {
					r.l.Unlock()
					return n, err
				}
			}

			for r.seek != base+blocksize && reterr == nil {
				block, reterr = r.getNextBlock()
			}
			cache.Wait()
			r.l.Unlock()
		}

		blockskip := min(len(block), max(0, int(off-base)))
		src := block[blockskip:]
		dst := buf[n:]
		if len(src) > len(dst) {
			reterr = nil // error is not applicable because it only attaches to the last byte of the block
		}
		n += copy(dst, src)
		if reterr != nil || n == len(buf) {
			break
		}
	}
	return n, reterr
}

func (r *ReaderAt) cacheKey(offset int64) string {
	return fmt.Sprintf("%s@%#x", r.uniq, offset)
}

func dbgCacheState(uniq string, upToBase int64) {
	matrix := make([]byte, upToBase/blocksize)
	for base := int64(0); base < upToBase; base += blocksize {
		key := fmt.Sprintf("%s@%#x", uniq, base)
		_, ok := cache.GetTTL(key)
		if ok {
			matrix[base/blocksize] = '*'
		} else {
			matrix[base/blocksize] = '-'
		}
	}
	os.Stdout.Write(matrix)
	os.Stdout.WriteString("\n")
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
