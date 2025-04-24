package flate

import (
	"bytes"
	goflate "compress/flate"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"testing"
)

var rawBin = mkTestBin()
var compressedBin = stdLibCompress(rawBin)

func TestFlate(t *testing.T) {
	rng := rand.New(rand.NewPCG(22, 22))
	var r *Reader
	for i := range 100 {
		left := rng.Int64N(int64(len(rawBin)))
		right := rng.Int64N(int64(len(rawBin)))
		left, right = min(left, right), max(left, right)

		t.Run(fmt.Sprintf("%#x:%#x fresh=%d", left, right, (i+1)%2), func(t *testing.T) {
			if i%2 == 0 {
				r = NewReader(bytes.NewReader(compressedBin), int64(len(compressedBin)), int64(len(rawBin)))
			}

			buf := make([]byte, right-left)
			n, err := r.ReadAt(buf, left)
			if err != nil && err != io.EOF {
				t.Error(err)
			}
			if n != int(right-left) {
				t.Errorf("expected %d bytes got %d", right-left, n)
			}
			if !bytes.Equal(buf, rawBin[left:right]) {
				t.Error("bad data")
				os.WriteFile("/tmp/a", buf, 0o644)
				os.WriteFile("/tmp/b", rawBin[left:right], 0o644)
			}
		})
	}
}

func mkTestBin() []byte {
	var r []byte
	rng := rand.New(rand.NewPCG(20121993, 0))
	for range 3 {
		for range 30000 {
			r = append(r, byte(rng.IntN(256)))
		}
		r = append(r, make([]byte, 10000)...)
		for range 5000 {
			r = append(r, r[len(r)-rng.IntN(19000)-1000:][:rng.IntN(1000)]...)
		}
	}
	fmt.Println(len(r), "is the size")
	return r
}

func stdLibCompress(b []byte) []byte {
	dest := bytes.NewBuffer(nil)
	cpr, _ := goflate.NewWriter(dest, 6)
	fmt.Println("compressing")
	_, err := cpr.Write(b)
	if err != nil {
		panic("could not compress data for tests")
	}
	cpr.Flush()
	return dest.Bytes()
}
