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

func (r *ReaderAt) advance(buf []byte) (n int, err error) {
	key := r.cacheKey(r.seek)
	n, err = io.ReadFull(r.r, buf)
	// fmt.Printf("  subread %#x + %#x = %#x, %v\n", r.seek, len(buf), n, err)
	// fmt.Println("     ", hex.EncodeToString(buf[:n]))
	r.seek += int64(n)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if err != nil {
		r.eof, r.err = r.seek, err
		r.close()
	}
	cache.Set(key, buf[:n], int64(n))
	cache.Wait()
	return n, err
}

// This just seems to be hopelessly buggy: it is saving corrupt data
func (r *ReaderAt) ReadAt(buf []byte, off int64) (n int, reterr error) {
	// fmt.Printf("%s: ReadAt %#x+%#x\n", r.uniq, off, len(buf))
	for base := off / blocksize * blocksize; base < off+int64(len(buf)); base += blocksize {
		var block []byte

		if b, ok := cache.Get(r.cacheKey(base)); ok { // easy path
			// fmt.Printf("%s: cache hit at %#x\n", r.uniq, base)
			block = b.([]byte)
			if base+int64(len(block)) == r.eof {
				reterr = r.err
			}
		} else {
			// fmt.Printf("%s: cache miss at %#x\n  already have: ", r.uniq, base)
			// dbgCacheState(r.uniq, base)

			r.l.Lock()
			if r.seek > base || r.r == nil {
				r.close()
				if err := r.open(); err != nil {
					r.l.Unlock()
					return n, err
				}
			}

			block = make([]byte, blocksize)
			var blkn int
			for r.seek != base+blocksize && reterr == nil {
				blkn, reterr = r.advance(block)
			}
			block = block[:blkn]
			r.l.Unlock()
		}
		// fmt.Println(hex.EncodeToString(block))

		blockskip := min(len(block), max(0, int(off-base)))
		// fmt.Printf("copying file+%#x (block %#x + %#x, and blocksize is %#x) to buf+%#x\n", off+int64(n), base, blockskip, len(block), n)
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
	// fmt.Printf("n=%#x, err=%v\n", n, reterr)
	return n, reterr
}

func (r *ReaderAt) cacheKey(offset int64) string {
	// fmt.Printf("cachekey = %s\n", fmt.Sprintf("%s@%#x", r.uniq, offset))
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
