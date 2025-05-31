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
			r := New(irreg{}, nil, 0, 255, "irreg")
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
					t.Errorf("expected to return \"%v\" at offset %d, got \"%v\"",
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

/*
Question now is, who is responsible for serialising the struct?
Because we plan to flatten it to RAM and even compress it

examples include a Huffman tree, one of those nasty Arsenic "models"

so... how to flatten the model? or should it merely be *saved*? I tend to think saved, because that's a proven-good flattening format.
(This x 1000, we need a good cooperation to save the reader state down-to-the-bit)
It *might* be that we just need to use reflection, and opportunistically squish any byte-slices that we find
Or an extension-interface with a Shrink() method
Or an extension-interface with a Serialize() method

BTW, should a BitReader be passed in? It's a messy structure, not always a useful one.

God these compression formats are a squalid nuisance.
*/

type irreg struct{}

func (irreg) Init(r io.ReaderAt, packsz int64, unpacksz int64) (sharedstate []byte, err error) {
	return nil, nil
}

func (irreg) Step(r io.ReaderAt, sharedstate []byte, priorstate []byte, wantnextstate bool) (unpack []byte, ownstate []byte, nextstate []byte, err error) {
	i := byte(0)
	if priorstate != nil {
		i = priorstate[0]
	}
	var accum []byte
	for {
		accum = append(accum, i)
		i++
		if isPrime(int(i)) || i == 0 {
			break
		}
	}
	return accum, nil, []byte{i}, nil
}

func isPrime(s int) bool {
	for fac := 2; ; fac++ {
		if s%fac == 0 {
			return false
		} else if fac*fac > s {
			return true
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
