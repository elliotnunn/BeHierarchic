// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdavfs

import (
	"path"
	"testing"
)

func TestSlashClean(t *testing.T) {
	testCases := []string{
		"",
		".",
		"/",
		"/./",
		"//",
		"//.",
		"//a",
		"/a",
		"/a/b/c",
		"/a//b/./../c/d/",
		"a",
		"a/b/c",
	}
	for _, tc := range testCases {
		got := slashClean(tc)
		want := path.Clean("/" + tc)
		if got != want {
			t.Errorf("tc=%q: got %q, want %q", tc, got, want)
		}
	}
}
