package main

import (
	"strings"
	"testing"
)

func TestXxx(t *testing.T) {
	ra := strings.NewReader("hello")
	buf := make([]byte, 2)

	n, err := ra.ReadAt(buf, 3)
	t.Error(n, err)
	n, err = ra.ReadAt(buf, 4)
	t.Error(n, err)
	n, err = ra.ReadAt(buf, 5)
	t.Error(n, err)
	n, err = ra.ReadAt(buf, 6)
	t.Error(n, err)
}
