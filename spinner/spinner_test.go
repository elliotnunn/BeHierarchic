package spinner

import (
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	shift = 6
	block = 1 << shift
)

var quantum = time.Millisecond * 50

type tediousReader struct {
	delay bool
	total int
	seek  int
}

// assumes a blocksize
func (r *tediousReader) Read(p []byte) (int, error) {
	for i := range p {
		if r.seek == r.total {
			return i, io.EOF
		}
		p[i] = byteAtOffset(int64(r.seek))
		r.seek++
	}
	if r.delay {
		time.Sleep(quantum)
	}
	return len(p), nil
}

func (r *tediousReader) Stat() (fs.FileInfo, error) {
	panic("Stat unimplemented")
}

func (r *tediousReader) Close() error {
	return nil
}

func byteAtOffset(offset int64) byte {
	return byte(offset / 256)
}

type fsys struct {
	openCount int
}

func (fsys *fsys) Open(name string) (fs.File, error) {
	slow := strings.HasPrefix(name, "slow")
	name = strings.TrimPrefix(name, "slow")
	name = strings.TrimPrefix(name, "fast")
	size, _ := strconv.Atoi(name)
	fsys.openCount++
	return &tediousReader{delay: slow, total: size}, nil
}

func TestConcurrent(t *testing.T) {
	whereToRead := []struct {
		offset int64
		size   int
		got    int
		err    error
	}{
		{0, 10, 10, nil},
		{block - 5, 5, 5, nil},
		{2*block - 5, 5, 5, nil},
		{2*block - 5, 4, 4, nil},
		{2*block - 5, 10, 10, nil},
		{2*block + 1, 10, 10, nil},
		{3*block - 10, 10, 10, nil},
		{3*block - 10, 12, 10, io.EOF},
		{3*block + 0, 1, 0, io.EOF},
		{4*block + 0, 1, 0, io.EOF},
	}

	fsys := new(fsys)
	pool := New(shift, 10, 10)
	f := pool.ReaderAt(fsys, fmt.Sprintf("slow%d", 3*block))

	t.Run("parallelreads", func(t *testing.T) {
		for _, testCase := range whereToRead {
			t.Run(fmt.Sprintf("%d,%d", testCase.offset, testCase.size), func(t *testing.T) {
				t.Parallel()
				buf := make([]byte, testCase.size)
				n, err := f.ReadAt(buf, testCase.offset)
				if testCase.got != n {
					t.Errorf("wrong length! expected %d got %d", testCase.got, n)
				}
				if testCase.err != err {
					t.Errorf("wrong error! expected %v got %v", testCase.err, err)
				}
				ok := true
				for i, c := range buf[:n] {
					if c != byteAtOffset(testCase.offset+int64(i)) {
						ok = false
					}
				}
				if !ok {
					t.Error("data mismatch")
				}
			})
		}
	})

	t.Run("openCount", func(t *testing.T) {
		if fsys.openCount != 1 {
			t.Errorf("expected %d got %d", 1, fsys.openCount)
		}
	})
}

func TestSerial(t *testing.T) {
	whereToRead := [][]struct {
		offset int64
		size   int
		got    int
		err    error
	}{
		{
			{0, 10, 10, nil},
			{0, 10, 10, nil},
		},
		{
			{block, 10, 10, nil},
			{0, 10, 10, nil},
			{block * 2, 10, 10, nil},
		},
	}

	for i, group := range whereToRead {
		fsys := new(fsys)
		pool := New(shift, 10, 10)
		f := pool.ReaderAt(fsys, fmt.Sprintf("fast%d", 3*block))

		t.Run(fmt.Sprintf("group%d", i), func(t *testing.T) {
			for _, testCase := range group {
				t.Run(fmt.Sprintf("%d,%d", testCase.offset, testCase.size), func(t *testing.T) {

					buf := make([]byte, testCase.size)
					n, err := f.ReadAt(buf, testCase.offset)
					if testCase.got != n {
						t.Errorf("wrong length! expected %d got %d", testCase.got, n)
					}
					if testCase.err != err {
						t.Errorf("wrong error! expected %v got %v", testCase.err, err)
					}
					ok := true
					for i, c := range buf[:n] {
						if c != byteAtOffset(testCase.offset+int64(i)) {
							ok = false
						}
					}
					if !ok {
						t.Error("data mismatch")
					}
				})
			}
		})
	}
}

func TestReadAhead(t *testing.T) {
	fsys := new(fsys)
	pool := New(shift, 10, 10)
	f := pool.ReaderAt(fsys, fmt.Sprintf("slow%d", 2*block))

	f.ReadAt(make([]byte, 10), 0)
	time.Sleep(quantum) // wait to fill the cache with the next block

	start := time.Now()
	_, err := f.ReadAt(make([]byte, 10), block)
	if time.Since(start) > quantum/4 {
		t.Error("lookahead negative")
	}
	if err != nil {
		t.Error(err)
	}
}
