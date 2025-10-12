package walk

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

func FilesInDiskOrder(fsys fs.FS) (string, <-chan string) {
	ret := make(chan string)
	switch t := fsys.(type) {
	default: // the exhaustive ReadDir case
		return sortPaths(fsys, walkAsync(fsys))
	case *zip.Reader:
		go func() {
			for _, f := range t.File {
				if f.Name != "" && !strings.HasSuffix(f.Name, "/") {
					ret <- f.Name
				}
			}
			close(ret)
		}()
		return "zip-file-order", ret
	}
}

func walkAsync(fsys fs.FS) <-chan string {
	ch, wg := make(chan string), new(sync.WaitGroup)
	wg.Add(1)
	go recurse(fsys, ".", ch, wg)
	go func() { wg.Wait(); close(ch) }()
	return ch
}

func recurse(fsys fs.FS, name string, ch chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	f, err := fsys.Open(name)
	if err != nil {
		return
	}
	defer f.Close()
	dir, ok := f.(fs.ReadDirFile)
	if !ok {
		panic(fmt.Sprintf("%q is a %T, does not satisfy ReadDirFile", name, f))
	}
	for {
		l, err := dir.ReadDir(10)
		for _, de := range l {
			switch de.Type() {
			case fs.ModeDir:
				wg.Add(1)
				go recurse(fsys, path.Join(name, de.Name()), ch, wg)
			case 0: // regular file
				ch <- path.Join(name, de.Name())
			}
		}
		if err != nil {
			return
		}
	}
}

// If there is no obvious sort key for the files,
// then the return will be synchronous
func sortPaths(fsys fs.FS, ch <-chan string) (string, <-chan string) {
	out := make(chan string)
	f1, ok := <-ch
	if !ok {
		close(out)
		return "no-files", out
	}

	var (
		k1      uint64
		waysort string
		cansort bool
	)
	stat1, err := fs.Stat(fsys, f1)
	if err != nil {
		waysort = err.Error()
	} else {
		k1, waysort, cansort = getkey(stat1)
		if !cansort {
			waysort = "walk-order"
		}
	}

	if cansort {
		go func() {
			defer close(out)
			sortlist := fileSlice{file{path: f1, info: stat1, key: k1}}
			for f := range ch {
				el := file{path: f}
				el.info, err = fs.Stat(fsys, f)
				if err == nil {
					el.key, _, _ = getkey(el.info)
				}
				sortlist = append(sortlist, el)
			}
			sort.Sort(sortlist)
			for _, f := range sortlist {
				out <- f.path
			}
		}()
		return waysort, out
	} else {
		go func() {
			defer close(out)
			out <- f1
			for f := range ch {
				out <- f
			}
		}()
		return waysort, out
	}
}

type fileSlice []file
type file struct {
	path string
	info fs.FileInfo
	key  uint64
}

func (x fileSlice) Len() int           { return len(x) }
func (x fileSlice) Less(i, j int) bool { return x[i].key < x[j].key }
func (x fileSlice) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }

func getkey(i fs.FileInfo) (uint64, string, bool) {
	if ino, ok := tryInode(i); ok { // intended as a vague proxy for "order on disk"
		return ino, "inode-number", true
	}

	switch t := i.Sys().(type) {
	case interface{ ByteOffset() int64 }:
		return uint64(t.ByteOffset()), "byte-offset", true
	case interface{ Inode() uint64 }:
		return t.Inode(), "inode-number", true
	}
	return 0, "", false
}

var tryInode = func(i fs.FileInfo) (uint64, bool) { return 0, false }
