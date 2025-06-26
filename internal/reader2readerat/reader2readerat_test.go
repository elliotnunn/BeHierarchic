// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package reader2readerat_test

import (
	"encoding/hex"
	"io"
	"math/rand"
	"testing"

	"github.com/elliotnunn/BeHierarchic/internal/reader2readerat"
)

type reader byte

func (r *reader) Read(p []byte) (n int, err error) {
	switch rand.Intn(3) {
	case 0:
		p = p[:len(p)-len(p)/2]
	case 1:
		p = nil
	case 2:
	}

	for i := range p {
		p[i] = byte(*r)
		*r++
	}
	return len(p), nil
}

func TestDecompressionCache(t *testing.T) {
	ra := reader2readerat.NewFromReader(func() (io.Reader, error) {
		var newreader reader
		return &newreader, nil
	})

	for range 100 {
		offset := int64(rand.Intn(1000))
		buf := make([]byte, rand.Intn(1000))
		n, err := ra.ReadAt(buf, offset)
		if err != nil {
			t.Errorf("got error %v", err)
		}
		if n != len(buf) {
			t.Errorf("expected %d bytes, got %d", len(buf), n)
		}
		for i, c := range buf[:n] {
			if c != byte(offset)+byte(i) {
				t.Errorf("expected to start with byte %02x, got %s", byte(offset), hex.EncodeToString(buf[:n]))
				break
			}
		}
	}
}
