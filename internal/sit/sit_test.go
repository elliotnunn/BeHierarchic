package sit

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"
)

//go:embed stuffit-test-files/sources
//go:embed proprietary-test
var sourcesFS embed.FS
var sources = fsToMap(sourcesFS)

// //go:embed stuffit-test-files/build
//
//go:embed proprietary-test
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
		t.Run(name, func(t *testing.T) {
			fmt.Println("\n", name, hex.EncodeToString(data[:16]))
			fs, err := New(bytes.NewReader(data))
			if err != nil {
				fmt.Println(err)
				return
			}
			dumpFS(fs)
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
			// fmt.Println(appledouble.Dump(f))
		}

		return nil
	})
}
