// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"fmt"
	"io/fs"
	gopath "path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
)

func dumpFS(fsys fs.FS) {
	const tfmt = "2006-01-02T15:04:05"
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		fmt.Printf("%#v\n", p)
		i, err := d.Info()
		if err != nil {
			fmt.Printf("    dump error: %s\n", err.Error())
			return fs.SkipDir
		}

		fmt.Printf("    %v size=%d modtime=%s\n",
			i.Mode(), i.Size(), i.ModTime().Format(tfmt))

		// AppleDouble file
		if strings.HasPrefix(gopath.Base(p), "._") {
			f, err := fsys.Open(p)
			if err != nil {
				fmt.Printf("    AppleDouble dump error: %s\n", err.Error())
				return nil
			}
			defer f.Close()
			dmp, err := appledouble.Dump(f)
			if err != nil {
				fmt.Printf("    AppleDouble dump error: %v\n", err)
			} else {
				for _, l := range strings.Split(dmp, "\n") {
					fmt.Printf("    %s\n", l)
				}
			}
		}

		return nil
	})
}
