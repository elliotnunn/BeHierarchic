package spinner

import (
	"fmt"
	"io"
	"testing"
	"time"
)

type tediousReader int

func (r *tediousReader) Read(p []byte) (int, error) {
	println("reading", len(p), "at", *r)
	for i := range p {
		p[i] = byteAtOffset(int64(*r))
		*r++
		if *r == 256*256 {
			return i, io.EOF
		}
	}
	time.Sleep(time.Millisecond * 100)
	return len(p), nil
}

func byteAtOffset(offset int64) byte {
	return byte(offset / 256)
}

func openFunc(id ID) (io.Reader, error) {
	println("reopen")
	return new(tediousReader), nil
}

func init() {
	OpenFunc = openFunc
}

func TestThatItWorks(t *testing.T) {
	whereToRead := []struct{ offset, size int64 }{
		{0, 10},
		{10*1024 - 5, 10},
		{0, 10},
		{10*1024 - 5, 10},
	}

	r := ID(0)
	for _, testCase := range whereToRead {
		t.Run(fmt.Sprint(testCase), func(t *testing.T) {
			t.Parallel()
			buf := make([]byte, testCase.size)
			n, err := r.ReadAt(buf, testCase.offset)
			if n != len(buf) {
				t.Errorf("wrong length! expected %d got %d", len(buf), n)
			}
			if err != nil {
				t.Error(err)
			}
			ok := true
			for i, c := range buf {
				if c != byteAtOffset(testCase.offset+int64(i)) {
					ok = false
				}
			}
			if !ok {
				t.Error("data mismatch")
			}
		})
	}
}
