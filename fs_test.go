// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"fmt"
	"os"
	"testing"
)

func TestFS(t *testing.T) {
	base := "/Users/elliotnunn/Downloads"
	concrete := os.DirFS(base)
	abstract := Wrapper(concrete)
	dumpFS(concrete)
	fmt.Println("------")
	dumpFS(abstract)
}
