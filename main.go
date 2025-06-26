package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	_ "net/http/pprof"

	"github.com/shurcooL/webdavfs/webdavfs"
	"golang.org/x/net/webdav"
)

const hello = "BeHierarchic: the Retrocomputing Archivist's File Server, by Elliot Nunn"

func main() {
	err := cmdLine(os.Args)
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}

func cmdLine(args []string) error {
	if len(args) != 3 {
		return errors.New(hello + "\n" + "Usage: BeHierarchic [INTERFACE:]PORT SHAREPOINT")
	}

	port := args[1]

	path := args[2]
	s, err := os.Stat(path)
	if err != nil {
		return err
	} else if !s.IsDir() {
		return fmt.Errorf("%s: not a directory", path)
	}
	fsys := Wrapper(os.DirFS(path))

	http.Handle("/", &webdav.Handler{
		FileSystem: webdavfs.New(http.FS(fsys)),
		LockSystem: webdav.NewMemLS(),
	})

	return http.ListenAndServe(port, nil)
}
