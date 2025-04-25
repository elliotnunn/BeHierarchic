package main

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/elliotnunn/resourceform/internal/hfs"
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

		// idea: make "cnid" more like "inode"?
		fmt.Printf("    name=%s\n",
			i.Name())
		fmt.Printf("    isdir=%v type=%v size=%d mode=%v modtime=%s\n",
			d.IsDir(), d.Type(), i.Size(), i.Mode(), i.ModTime().Format(tfmt))

		if s, ok := i.Sys().(*hfs.Sys); ok {
			fmt.Printf("    cnid=%d flags=%#04x crtime=%s bktime=%s\n",
				s.ID, s.Flags, s.CreationTime.Format(tfmt), s.BackupTime.Format(tfmt))
			fmt.Printf("    %s\n", hex.EncodeToString(s.FinderInfo[:]))
		}

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
