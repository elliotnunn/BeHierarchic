// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unsafe"

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

func searchPage(fsys *FS, w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("q")
	if !doublestar.ValidatePattern(pattern) {
		http.Error(w, "not a valid glob pattern", http.StatusNotFound)
		return
	}

	searchroot := strings.TrimSuffix(r.URL.Path, "/.glob.html")
	searchroot = strings.TrimPrefix(searchroot, "/")
	if searchroot == "" {
		searchroot = "."
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

	// cutleft := len(searchroot) + 1
	// if searchroot == "." {
	// 	cutleft = 0
	// }

	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for buf := range o.glob(pattern) {
		bw.WriteString(`<a href="/`)
		httpEscapePath(bw, buf)
		bw.WriteString(`">`)
		htmlReplacer.WriteString(bw, unsafeString(buf))
		bw.WriteString(`</a>` + "\n")
		n++
		if n == 2000 {
			fmt.Fprintln(bw, "Limited results")
			break
		}
	}
	fmt.Fprintf(bw, "%d results in %s", n, time.Since(t))
}

func unsafeString(s []byte) string {
	return unsafe.String(&s[0], len(s))
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

func httpEscapePath(w io.ByteWriter, s []byte) {
	for _, c := range s {
		switch {
		case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == '/':
			w.WriteByte(c)
		default:
			w.WriteByte('%')
			w.WriteByte("0123456789ABCDEF"[c/16])
			w.WriteByte("0123456789ABCDEF"[c%16])
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
