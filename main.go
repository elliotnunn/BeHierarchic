// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	_ "net/http/pprof"

	"github.com/elliotnunn/BeHierarchic/internal/webdavfs"
)

const hello = `BeHierarchic, the Retrocomputing Archivist's File Server

Usage:  BeHierarchic [INTERFACE:]PORT SHAREPOINT

(use the BEGB environment variable to set the cache size in GiB)`

func main() {
	err := cmdLine(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func cmdLine(args []string) error {
	if len(args) != 3 {
		return errors.New(hello)
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

	http.Handle("/", &webdavfs.Handler{FS: fsys})

	return http.ListenAndServe(port, nil)
}
