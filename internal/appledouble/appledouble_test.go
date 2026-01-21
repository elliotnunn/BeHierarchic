// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package appledouble

import (
	"bytes"
	"io"
	"math"
	"strings"
	"testing"
	"testing/iotest"
)

func TestResourceForkPassthruSequential(t *testing.T) {
	const data = "hello this is a fork"

	var ad AppleDouble
	rfFunc := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil }
	rfFunc, rfSize := ad.WithSequentialResourceFork(rfFunc, int64(len(data)))
	allData, _ := rfFunc()

	rf := allData.(*reader)
	expect := append(rf.ad, make([]byte, rf.zero)...)
	expect = append(expect, data...)

	if rfSize != int64(len(expect)) {
		t.Error("size mismatch", rfSize, len(expect))
	}

	err := iotest.TestReader(allData, expect)
	if err != nil {
		t.Error(err)
	}
}

func TestResourceForkPassthru(t *testing.T) {
	const data = "hello this is a forker"

	var ad AppleDouble
	rf, rfSize := ad.WithResourceFork(strings.NewReader(data), int64(len(data)))

	got, err := io.ReadAll(io.NewSectionReader(rf, 0, math.MaxInt64))
	if len(got) != int(rfSize) {
		t.Error("expected", rfSize, "got", len(got))
	}
	if err != nil {
		t.Error(err)
	}
	if !bytes.HasSuffix(got, []byte(data)) {
		t.Error("does not end with data")
	}
}

func TestReaderAt(t *testing.T) {
	const data = "hello this is a forker"

	var ad AppleDouble
	rf, _ := ad.WithResourceFork(strings.NewReader(data), int64(len(data)))

	expect := append(rf.(*readerAt).ad, data...)

	for off := range len(expect) - 1 {
		var buf [2]byte
		n, err := rf.ReadAt(buf[:], int64(off))
		if n != 2 ||
			string(buf[:]) != string(expect[off:][:2]) ||
			off != len(expect)-2 && err != nil {
			t.Error(off, n, err)
		}
	}
}
