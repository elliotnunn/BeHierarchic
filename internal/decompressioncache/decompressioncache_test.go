package decompressioncache

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

func TestDecompressionCache(t *testing.T) {
	type span struct{ offset, len int }
	spans := []span{
		{0, 1},
		{0, 3},
		{50, 10},
		{50, 30},
		{200, 55},
		{200, 56},
	}

	const expectlen = 255

	permute(spans, func(spans []span) {
		t.Run(fmt.Sprint(spans), func(t *testing.T) {
			r := New(StartIrreg(), "irregular")
			for _, span := range spans {
				bin := make([]byte, span.len)
				n, err := r.ReadAt(bin, int64(span.offset))

				expectn := min(span.len, expectlen-span.offset)
				if expectn != n {
					t.Errorf("expected to read %d bytes at offset %d, got %d",
						expectn, span.offset, n)
				}

				var expecterr error
				if span.offset+span.len >= expectlen {
					expecterr = io.EOF
				}
				if expecterr != err {
					t.Errorf("expected to return \"%g\" at offset %d, got \"%g\"",
						expecterr, span.offset, err)
				}

				expectbin := make([]byte, n)
				for i := range expectbin {
					expectbin[i] = byte(span.offset + i)
				}
				if !bytes.Equal(expectbin, bin[:n]) {
					t.Errorf("expected to read \"%s\" at offset %d, got \"%s\"",
						hex.EncodeToString(expectbin), span.offset, hex.EncodeToString(bin[:n]))
				}
			}
		})
	})
}

// Counts 0 to
func StartIrreg() Stepper {
	return func() (Stepper, []byte, error) { return stepIrreg(0) }
}

func stepIrreg(s int) (Stepper, []byte, error) {
	var ret []byte

	for {
		ret = append(ret, byte(s))

		isPrime := true
		for fac := 2; ; fac++ {
			if s%fac == 0 {
				isPrime = false
				break
			} else if fac*fac > s {
				break
			}
		}
		s++

		// Freeze our state in a reusable closure
		stepper := func() (Stepper, []byte, error) { return stepIrreg(s) }
		if s == 255 {
			return stepper, ret, io.EOF
		} else if isPrime {
			return stepper, ret, nil
		}
	}
}

func permute[T any](arr []T, f func([]T)) {
	permuteHelper(arr, f, 0)
}

func permuteHelper[T any](arr []T, f func([]T), i int) {
	if i > len(arr) {
		f(arr)
		return
	}
	if i == len(arr) {
		f(arr)
		return
	}
	for j := i; j < len(arr); j++ {
		arr[i], arr[j] = arr[j], arr[i]
		permuteHelper(arr, f, i+1)
		arr[i], arr[j] = arr[j], arr[i] // backtrack
	}
}
