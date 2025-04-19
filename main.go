/*
Open problems:
fork format (the obvious one)
handling of more ephemeral Finder info, like is-inited and has-custom-icon?
UTF-8 <-> Macintosh format conversions
aliases <-> symlinks???
*/

package main

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"testing/fstest"

	"github.com/elliotnunn/resourceform/internal/hfs"
)

type Fmt byte

const (
	Rez Fmt = iota
	Darwin
	AppleDouble
	AppleSingle
	HQX
	MBIN
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

	// err := fstest.TestFS(abstract, "System 7.0 HD.dsk")
	// fmt.Println(err)

	// o, err := abstract.Open("nonexistent")
	// fmt.Println(o, err)
	// fmt.Println("--------------")

	dumpFS(concrete)
	fmt.Println("--------------")
	dumpFS(abstract)
	fmt.Println("--------------")
	err := fstest.TestFS(abstract, "System 7.0 HD.dsk")
	fmt.Println(err)
}

// func main() {
// 	base := os.Args[1]

// 	disk, err := os.Open(base)
// 	if err != nil {
// 		os.Stdout.WriteString(err.Error() + "\n")
// 		os.Exit(1)
// 	}

// 	fsys, err := hfs.New(disk)
// 	if err != nil {
// 		os.Stdout.WriteString(err.Error() + "\n")
// 		os.Exit(1)
// 	}
// 	dumpFS(fsys)

// 	// fsys2 := os.DirFS("/Users/elliotnunn/mac/resourceform")
// 	// dumpFS(fsys2)

// 	// f, err := fsys.Open("Macintosh HD/System Folder/System" + ResForkSuffix)
// 	// if err != nil {
// 	// 	panic(err)
// 	// }
// 	// text, err := io.ReadAll(f)
// 	// if err != nil {
// 	// 	panic(err)
// 	// }
// 	// fmt.Println(string(deRez(text)))

// 	// var wg sync.WaitGroup
// 	//
// 	//	filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
// 	//		name := filepath.Base(path)
// 	//
// 	//		if name[0] == '.' || strings.HasSuffix(name, ".rdump") || strings.HasSuffix(name, ".idump") {
// 	//			if d.IsDir() {
// 	//				return fs.SkipDir
// 	//			} else {
// 	//				return nil
// 	//			}
// 	//		}
// 	//
// 	//		if d.IsDir() {
// 	//			return nil
// 	//		}
// 	//
// 	//		println(path)
// 	//
// 	//		wg.Add(1)
// 	//		go func() {
// 	//			cvt(path, Darwin, Rez)
// 	//			wg.Done()
// 	//		}()
// 	//		return nil
// 	//	})
// 	//
// 	// wg.Wait()
// }
