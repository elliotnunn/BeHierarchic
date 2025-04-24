package flate

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

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

	got, err := io.ReadAll(NewReader(bytes.NewReader(z)))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, expect) {
		t.Fatalf("mismatch expect %d got %d bytes", len(expect), len(got))
	}
}
