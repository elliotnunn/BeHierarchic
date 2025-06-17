package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
)

func dumpFS(fsys fs.FS) {
	const tfmt = "2006-01-02T15:04:05"
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		fmt.Printf("%#v\n", p)
		if d == nil {
			fmt.Println("    nil info!")
			return nil
		}

		i, err := d.Info()
		if err != nil {
			panic(err)
		}

		fmt.Printf("    %v size=%d modtime=%s\n",
			i.Mode(), i.Size(), i.ModTime().Format(tfmt))

		// AppleDouble file
		if strings.HasPrefix(path.Base(p), "._") {
			f, err := fsys.Open(p)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			dmp, err := appledouble.Dump(f)
			if err != nil {
				fmt.Printf("AppleDouble dump error: %v\n", err)
			} else {
				for _, l := range strings.Split(dmp, "\n") {
					fmt.Printf("    %s\n", l)
				}
			}
		}

		return nil
	})
}

func main() {
	base := os.Args[1]
	concrete := os.DirFS(base)
	abstract := Wrapper(concrete)

	go dumpFS(abstract)
	http.ListenAndServe(":1993", http.FileServerFS(abstract))
}
