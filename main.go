package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
)

func dumpFS(fsys fs.FS) {
	const tfmt = "2006-01-02T15:04:05"
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		fmt.Printf("%#v\n", path)
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

		return nil
	})
}

func main() {
	base := os.Args[1]
	concrete := os.DirFS(base)
	abstract := Wrapper(concrete)

	go func() {
		dumpFS(concrete)
		fmt.Println("----")
		dumpFS(abstract)
	}()
	http.ListenAndServe(":1993", http.FileServerFS(abstract))
}
