// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package reader2readerat

import (
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"weak"

	"github.com/maypok86/otter/v2"
)

type ReaderAt struct {
	r     io.Reader
	uniq  any
	path  string
	open  func() error
	close func()
	l     sync.Mutex
	seek  int64
	eof   int64
	err   error
}

// If the io.Reader is an io.ReadCloser then it will be closed when I am closed
func NewFromReader(uniq any, f func() (io.Reader, error)) *ReaderAt {
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

func NewFromReadSeeker(uniq any, from io.ReadSeeker) *ReaderAt {
	r := initCommon(uniq)
	r.open = func() error {
		_, err := from.Seek(0, io.SeekStart)
		r.r = from
		return err
	}
	r.close = r.closeCommon
	return r
}

func initCommon(uniq any) *ReaderAt {
	r := &ReaderAt{
		uniq: uniq,
	}
	if r.uniq == nil {
		r.uniq = weak.Make(r)
	}
	return r
}

func (r *ReaderAt) closeCommon() {
	r.r, r.seek = nil, 0
}

func (r *ReaderAt) getNextBlock() ([]byte, error) {
	buf := make([]byte, blocksize)
	key := cacheKey{r.uniq, r.seek}
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
	cache.Set(key, buf)
	return buf, err
}

func (r *ReaderAt) ReadAt(buf []byte, off int64) (n int, reterr error) {
	for base := off / blocksize * blocksize; base < off+int64(len(buf)); base += blocksize {
		k := cacheKey{r.uniq, base}
		keg, ok := cache.GetEntry(k)
		var block []byte
		if ok { // easy path
			block = keg.Value
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

func (r *ReaderAt) Close() error {
	r.close()
	return nil
}

func ClearCache() {
	cache.InvalidateAll()
}

var cache = otter.Must(&otter.Options[cacheKey, []byte]{
	MaximumSize: cacheMemLimit(),
})

const (
	blocksize = 4096
)

type cacheKey struct {
	file   any
	offset int64
}

func cacheMemLimit() int {
	if e := os.Getenv("BEGB"); e != "" {
		f, err := strconv.ParseFloat(e, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			panic("malformed BEGB environment variable, should be a number of gigabytes: " + e)
		}
		return int(f * 1024 * 1024 * 1024 / blocksize)
	}
	return 1 << 30 // fall back on 1GiB
}
