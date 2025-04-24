package flate

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

/*
So what do I actually want to do?
- Every Read call should be satisfied by creating a memory buffer
of the desired size plus a bit extra, and extracting that.

- An interesting problem is when we scan far forward: there should be a
"breadcrumb trail" kept of the block-bit-offset and the dictionary state at that spot.

type resumePoint struct {
	data []byte // 32kb of dict +/- a healthy amount of data after that point
	blockByte int64
	blockBit uint8
}

from the outside takes a ReaderAt, from the inside is essentially byteReader

// BTW, is there any scope for passthrough? probably not...

func readAtLeast(rp resumePoint, minLen int) ([]byte, resumePoint, error)

*/

func TestFlate(t *testing.T) {
	f, err := os.Open("/Users/elliotnunn/Documents/mac/primary/macworks sources.txt.zip")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.Stat()
	arch, _ := zip.NewReader(f, s.Size())
	zf, _ := arch.File[0].OpenRaw()
	z, _ := io.ReadAll(zf)
	fmt.Println(len(z))

	os.WriteFile("/Users/elliotnunn/Documents/mac/primary/macworks sources.txt.deflate", z, 0o644)

	expect, err := os.ReadFile("/Users/elliotnunn/Documents/mac/primary/macworks sources.txt")
	if err != nil {
		t.Fatal(err)
	}

	got := NewReader(bytes.NewReader(z)).ReadAll()

	os.WriteFile("/Users/elliotnunn/Documents/mac/primary/macworks sources.txt.got", got, 0o644)

	if !bytes.Equal(got, expect) {
		t.Fatalf("mismatch expect %d got %d bytes", len(expect), len(got))
	}
}
