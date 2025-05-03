package sit

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/elliotnunn/resourceform/internal/appledouble"
)

//go:embed stuffit-test-files/sources
var sourcesFS embed.FS
var sources = fsToMap(sourcesFS)

//go:embed stuffit-test-files/build
var archivesFS embed.FS
var archives = fsToMap(archivesFS)

func fsToMap(fsys fs.FS) map[string][]byte {
	ret := make(map[string][]byte)
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			ret[d.Name()], _ = fs.ReadFile(fsys, path)
		}
		return nil
	})
	return ret
}

func TestDumpEach(t *testing.T) {
	for name := range archives {
		data := archives[name]
		// if string(data[10:14]) == "rLau" {
		// 	continue
		// }
		t.Run(name, func(t *testing.T) {
			fsys, err := New(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}

			fmt.Println("##", name)
			dumpFS(fsys)
			fmt.Println("")
		})
	}
}

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
			d, _ := appledouble.Dump(f)
			for _, l := range strings.Split(strings.TrimRight(d, "\n"), "\n") {
				fmt.Println("    " + l)
			}
		}

		return nil
	})
}
