package hfs

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestMultiReaderAt(t *testing.T) {
	str := "0123456789a"

	r := multiReaderAt{
		backing: strings.NewReader("6789345120"),
		extents: []int64{9, 1, 7, 2, 4, 3, 0, 4},
	}

	for left := 0; left <= 11; left++ {
		for right := left; right <= 11; right++ {
			t.Run(fmt.Sprintf("%d:%d", left, right), func(t *testing.T) {
				buf := make([]byte, right-left)
				_, err := r.Seek(int64(left), io.SeekStart)
				if err != nil {
					t.Fatalf("seek error %e", err)
				}

				expect := strings.TrimRight(str[left:right], "a")
				expectEOF := right >= 10

				n, err := r.Read(buf)
				if n != len(expect) {
					t.Errorf("n should be %d but is %d", len(expect), n)
				}

				if n != len(expect) {
					t.Errorf("expected %d bytes but got %d", len(expect), n)
				}

				if err == nil {
					if expectEOF {
						t.Error("expected EOF but got success")
					}
				} else if errors.Is(err, io.EOF) {
					if !expectEOF {
						t.Error("expected success but got EOF")
					}
				} else {
					t.Error(err)
				}

				if string(buf[:n]) != expect {
					t.Errorf("expected %q but got %q", expect, string(buf[:n]))
				}
			})
		}
	}
}
