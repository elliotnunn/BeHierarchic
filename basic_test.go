// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"embed"
	"testing"
	"testing/fstest"
)

//go:embed testdata
var image embed.FS

func TestFS(t *testing.T) {
	fsys := Wrapper(image)
	fsys.Prefetch()
	err := fstest.TestFS(fsys, "testdata/archive.tgz◆/archive.tar◆/archive.zip◆/disk.img◆/Macintosh HD/hello world.txt")
	if err != nil {
		t.Error(err)
	}
}
