// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "net/http/pprof"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/elliotnunn/BeHierarchic/internal/webdavfs"
)

const hello = `BeHierarchic, the Retrocomputing Archivist's File Server

Usage:  BeHierarchic [INTERFACE:]PORT SHAREPOINT

(use the BEGB environment variable to set the RAM block-cache size in GiB,
 and the BECACHE environment variable to the on-disk cache path)`

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

	target := args[2]
	s, err := os.Stat(target)
	if err != nil {
		return err
	} else if !s.IsDir() {
		return fmt.Errorf("%s: not a directory", target)
	}

	fsys := Wrapper(os.DirFS(target), os.Getenv("BECACHE"))
	go fsys.Prefetch()

	webdav := webdavfs.Handler{FS: fsys}
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/.glob.html"):
			searchPage(fsys, w, r)
		case (r.Method == "GET" || r.Method == "HEAD") && strings.HasSuffix(r.URL.Path, "/"):
			dirPage(fsys, w, r)
		default:
			webdav.ServeHTTP(w, r)
		}
	}))
	return http.ListenAndServe(port, nil)
}

func dirPage(fsys *FS, w http.ResponseWriter, r *http.Request) {
	pathname := strings.Trim(r.URL.Path, "/")
	if pathname == "" {
		pathname = "."
	}

	f, err := fsys.Open(pathname)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	d, ok := f.(fs.ReadDirFile)
	if !ok {
		http.Error(w, "could not assert fs.ReadDirFile", 404)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html>\n")
	fmt.Fprintf(w, "<meta name=\"viewport\" content=\"width=device-width\">\n")
	fmt.Fprint(w, "<h1>BeHierarchic</h1>")
	fmt.Fprint(w, "<h2>")
	breadcrumb(w, pathname)
	fmt.Fprint(w, "</h2>")
	fmt.Fprintf(w, `<form action=".glob.html" method="GET">`+
		`<input type="text" name="q" size="50" placeholder="Pattern e.g. **/*.sit">`+
		`<button type="submit">Glob Search</button></form>`)
	fmt.Fprintf(w, "<pre>")
	for {
		list, err := d.ReadDir(100)
		for _, de := range list {
			slash := ""
			if de.IsDir() {
				slash = "/"
			}
			fmt.Fprintf(w, `<a href="%s%s%s">%s%s</a>`+"\n",
				r.URL.Path, urlenc(de.Name()), slash,
				htmlReplacer.Replace(de.Name()), slash)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Println(htmlReplacer.Replace(err.Error()))
		}
	}
}

func rel(child, parent string) string {
	if parent == child {
		return "."
	} else if parent == "." {
		return child
	} else if len(child) > len(parent) && child[:len(parent)] == parent && child[len(parent)] == '/' {
		return child[len(parent)+1:]
	} else {
		return ""
	}
}

func searchPage(fsys *FS, w http.ResponseWriter, r *http.Request) {
	searchroot := strings.TrimSuffix(r.URL.Path, "/.glob.html")
	searchroot = strings.TrimPrefix(searchroot, "/")
	if searchroot == "" {
		searchroot = "."
	}

	pattern := r.URL.Query().Get("q")
	if !doublestar.ValidatePattern(pattern) {
		http.Error(w, "not a valid glob pattern", http.StatusNotFound)
		return
	}

	o, err := fsys.path(searchroot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html>\n")
	fmt.Fprintf(w, "<meta name=\"viewport\" content=\"width=device-width\">\n")
	fmt.Fprint(w, "<h1>BeHierarchic Search</h1>")
	fmt.Fprint(w, "<h2>")
	breadcrumb(w, searchroot)
	fmt.Fprint(w, "</h2>")
	fmt.Fprintf(w, `<form action=".glob.html" method="GET">`+
		`<input type="text" name="q" value="%s" size="50" placeholder="Pattern e.g. **/*.sit">`+
		`<button type="submit">Glob Search</button></form>`,
		pattern)
	fmt.Fprintf(w, "<pre>")

	n := 0
	t := time.Now()
	for o, kind := range o.deepWalk() {
		pathname := o.String()
		relpath := rel(pathname, searchroot)
		if relpath == "" || relpath == "." {
			continue
		}
		matches := doublestar.MatchUnvalidated(pattern, relpath)
		if !matches && kind.IsDir() {
			matches = doublestar.MatchUnvalidated(pattern, relpath+"/")
		}
		if !matches {
			continue
		}
		if kind.IsDir() {
			relpath += "/"
		}

		fmt.Fprintf(w, `<a href="/%s">%s</a>`+"\n",
			urlenc(pathname),
			htmlReplacer.Replace(relpath))

		n++
		if n == 2000 {
			fmt.Fprintln(w, "Limited results")
			break
		}
	}
	fmt.Fprintf(w, "%d results in %s", n, time.Since(t))
}

func breadcrumb(w io.Writer, path string) {
	fmt.Fprint(w, `<a href="/">/</a>`)
	if path != "." {
		steps := strings.Split(path, "/")
		for i := range steps {
			url := strings.Join(steps[:i+1], "/")
			fmt.Fprintf(w, "<a href=\"/%s\">%s</a>/", urlenc(url), htmlReplacer.Replace(steps[i]))
		}
	}
}

func urlenc(s string) string {
	url := url.URL{Path: s}
	return url.String()
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
	"\r", `\r`,
	"\n", `\n`,
)
