// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"path"
	"strings"
)

// link is already an absolute path
// target follows the Unix conventions for symlink targets
// if ".."s would escape from the root the return an empty string
func CleanLinkTarget(link, target string) string {
	var p string
	if strings.HasPrefix(target, "/") {
		p = path.Clean(target[1:])
	} else {
		p = path.Join(link, "..", target)
	}
	if p == link || strings.HasPrefix(p, link+"/") {
		return "" // points to self or child of self
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "" // points outside root
	}
	return p
}
