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
	rfFunc, rfSize := ad.WithSequentialResourceFork(func() io.Reader { return strings.NewReader(data) }, int64(len(data)))

	got, err := io.ReadAll(rfFunc())
	if len(got) != int(rfSize) {
		t.Error("wrong size")
	}
	if err != nil {
		t.Error(err)
	}
	if !bytes.HasSuffix(got, []byte(data)) {
		t.Error("does not end with data")
	}
}

func TestReader(t *testing.T) {
	const data = "hello this is a fork"

	var ad AppleDouble
	rfFunc, rfSize := ad.WithSequentialResourceFork(func() io.Reader { return strings.NewReader(data) }, int64(len(data)))
	rf := rfFunc().(*reader)

	expect := append(rf.ad, make([]byte, rf.zero)...)
	expect = append(expect, data...)
	if len(expect) != int(rfSize) {
		t.Error("wrong size")
	}

	err := iotest.TestReader(rfFunc(), expect)
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
	rf, rfSize := ad.WithResourceFork(strings.NewReader(data), int64(len(data)))

	expect := append(rf.(*readerAt).ad, data...)

	err := iotest.TestReader(io.NewSectionReader(rf, 0, rfSize), expect)
	if err != nil {
		t.Error(err)
	}
}
