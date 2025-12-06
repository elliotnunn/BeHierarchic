// Copyright 2014 The Go Authors. All rights reserved.
// Simplified 2025 Elliot Nunn
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package webdavfs provides a WebDAV server around a fs.FS.
package webdavfs // import "github.com/elliotnunn/BeHierarchic/internal/webdav"

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

type Handler struct {
	// FS is the virtual file system.
	FS fs.FS
	// Logger is an optional error logger. If non-nil, it will be called
	// for all HTTP requests.
	Logger func(*http.Request, error)
}

func pathConvert(p string) (string, int, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		p = "."
	}
	return p, http.StatusOK, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, err := http.StatusBadRequest, errUnsupportedMethod
	if h.FS == nil {
		status, err = http.StatusInternalServerError, errNoFileSystem
	} else {
		switch r.Method {
		case "OPTIONS":
			status, err = h.handleOptions(w, r)
		case "GET", "HEAD":
			status, err = h.handleGetHead(w, r)
		case "PROPFIND":
			status, err = h.handlePropfind(w, r)
		case "DELETE", "POST", "PUT", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK", "PROPPATCH":
			status, err = http.StatusMethodNotAllowed, nil
		}
	}

	if status != 0 {
		w.WriteHeader(status)
		if status != http.StatusNoContent {
			w.Write([]byte(StatusText(status)))
		}
	}
	if h.Logger != nil {
		h.Logger(r, err)
	}
}

func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := pathConvert(r.URL.Path)
	if err != nil {
		return status, err
	}
	allow := "OPTIONS"
	if fi, err := fs.Stat(h.FS, reqPath); err == nil {
		if fi.IsDir() {
			allow = "OPTIONS, PROPFIND"
		} else {
			allow = "OPTIONS, PROPFIND, GET"
		}
	}
	w.Header().Set("Allow", allow)
	// http://www.webdav.org/specs/rfc4918.html#dav.compliance.classes
	w.Header().Set("DAV", "1") // locking not supported
	// http://msdn.microsoft.com/en-au/library/cc250217.aspx
	w.Header().Set("MS-Author-Via", "DAV")
	return 0, nil
}

func (h *Handler) handleGetHead(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := pathConvert(r.URL.Path)
	if err != nil {
		return status, err
	}
	if path.Base(reqPath) == "besearch" {
		return h.searchPage(w, r)
	}
	f, err := h.FS.Open(reqPath)
	if err != nil {
		return http.StatusNotFound, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return http.StatusNotFound, err
	}
	if fi.IsDir() {
		return dirList(w, f.(fs.ReadDirFile), reqPath)
	}
	if _, ok := f.(io.ReadSeeker); !ok {
		slog.Error("neitherDirNorSeekReader", "type", reflect.TypeOf(f), "path", reqPath)
		return http.StatusInternalServerError, err
	}
	etag, err := findETag(h.FS, reqPath, fi)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", fi.ModTime(), f.(io.ReadSeeker))
	return 0, nil
}

func (h *Handler) searchPage(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, _, _ := pathConvert(r.URL.Path)
	reqPath = path.Dir(reqPath)
	pattern := r.URL.Query()
	glob := pattern.Get("q")
	if glob == "" {
		return http.StatusBadRequest, errors.New("no query")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html>\n")
	fmt.Fprintf(w, "<meta name=\"viewport\" content=\"width=device-width\">\n")
	fmt.Fprint(w, "<h1>BeHierarchic Search</h1>")
	fmt.Fprint(w, "<h2>")
	breadcrumb(w, reqPath)
	fmt.Fprint(w, "</h2>")
	fmt.Fprintf(w, `<form action="/%s" method="GET"><input type="text" name="q" value="%s"><button type="submit">Search</button></form>`,
		urlenc(path.Join(reqPath, "besearch")), htmlReplacer.Replace(glob))
	fmt.Fprintf(w, "<pre>\n")

	err = fs.WalkDir(h.FS, reqPath, func(name string, d fs.DirEntry, err error) error {
		ok, err := path.Match(glob, path.Base(name))
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if d.IsDir() {
			name += "/"
		}

		name = name[len(reqPath)+1:]

		fmt.Fprintf(w, "<a href=\"%s\">%s</a>\n", urlenc(name), htmlReplacer.Replace(name))
		return nil
	})
	if err != nil {
		return http.StatusBadRequest, err // bad glob
	}
	fmt.Fprintf(w, "</pre>\n")
	return 0, nil
}

func breadcrumb(w io.Writer, path string) {
	println("breadcrumbs for", path)
	steps := strings.Split(path, "/")
	for i := range steps {
		url := strings.Join(steps[:i+1], "/")
		fmt.Fprintf(w, "<a href=\"/%s\">%s</a>/", urlenc(url), htmlReplacer.Replace(steps[i]))
	}
}

func urlenc(s string) string {
	url := url.URL{Path: s}
	return url.String()
}

func dirList(w http.ResponseWriter, d fs.ReadDirFile, pathname string) (status int, err error) {
	list, err := d.ReadDir(-1)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	slices.SortFunc(list, func(i, j fs.DirEntry) int { return strings.Compare(i.Name(), j.Name()) })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html>\n")
	fmt.Fprintf(w, "<meta name=\"viewport\" content=\"width=device-width\">\n")
	fmt.Fprint(w, "<h1>BeHierarchic</h1>")
	fmt.Fprint(w, "<h2>")
	breadcrumb(w, pathname)
	fmt.Fprint(w, "</h2>")
	fmt.Fprintf(w, `<form action="/%s" method="GET"><input type="text" name="q"><button type="submit">Search</button></form>`,
		urlenc(path.Join(pathname, "besearch")))
	fmt.Fprintf(w, "<pre>\n")
	for _, de := range list {
		name := de.Name()
		if de.IsDir() {
			name += "/"
		}
		// name may contain '?' or '#', which must be escaped to remain
		// part of the URL path, and not indicate the start of a query
		// string or fragment.
		fmt.Fprintf(w, "<a href=\"/%s\">%s</a>\n", urlenc(path.Join(pathname, name)), htmlReplacer.Replace(name))
	}
	fmt.Fprintf(w, "</pre>\n")
	return 0, nil
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
)

func (h *Handler) handlePropfind(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := pathConvert(r.URL.Path)
	if err != nil {
		return status, err
	}
	fi, err := fs.Stat(h.FS, reqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return http.StatusNotFound, err
		}
		return http.StatusMethodNotAllowed, err
	}
	var depth int
	switch r.Header.Get("Depth") {
	case "0":
		depth = 0
	case "1":
		depth = 1
	default:
		return http.StatusBadRequest, errInvalidDepth
	}
	pf, status, err := readPropfind(r.Body)
	if err != nil {
		return status, err
	}

	mw := multistatusWriter{w: w}

	walkFn := func(reqPath string, info os.FileInfo, err error) error {
		if err != nil {
			return handlePropfindError(err, info)
		}

		var pstats []Propstat
		if pf.Propname != nil {
			pnames, err := propnames(h.FS, reqPath)
			if err != nil {
				return handlePropfindError(err, info)
			}
			pstat := Propstat{Status: http.StatusOK}
			for _, xmlname := range pnames {
				pstat.Props = append(pstat.Props, property{XMLName: xmlname})
			}
			pstats = append(pstats, pstat)
		} else if pf.Allprop != nil {
			pstats, err = allprop(h.FS, reqPath, pf.Prop)
		} else {
			pstats, err = props(h.FS, reqPath, pf.Prop)
		}
		if err != nil {
			return handlePropfindError(err, info)
		}
		href := reqPath
		if href == "." {
			href = ""
		} else if info.IsDir() {
			href += "/"
		}
		href = "/" + href
		return mw.write(makePropstatResponse(href, pstats))
	}

	walkErr := walkFS(h.FS, depth, reqPath, fi, walkFn)
	closeErr := mw.close()
	if walkErr != nil {
		return http.StatusInternalServerError, walkErr
	}
	if closeErr != nil {
		return http.StatusInternalServerError, closeErr
	}
	return 0, nil
}

func makePropstatResponse(href string, pstats []Propstat) *response {
	resp := response{
		Href:     []string{(&url.URL{Path: href}).EscapedPath()},
		Propstat: make([]propstat, 0, len(pstats)),
	}
	for _, p := range pstats {
		var xmlErr *xmlError
		if p.XMLError != "" {
			xmlErr = &xmlError{InnerXML: []byte(p.XMLError)}
		}
		resp.Propstat = append(resp.Propstat, propstat{
			Status:              fmt.Sprintf("HTTP/1.1 %d %s", p.Status, StatusText(p.Status)),
			Prop:                p.Props,
			ResponseDescription: p.ResponseDescription,
			Error:               xmlErr,
		})
	}
	return &resp
}

func handlePropfindError(err error, info os.FileInfo) error {
	// The x/net/webdav behaviour was to abort the PROPFIND on a Stat/ReadDir/etc error.
	// This causes the client to miss out on a lot of useful information.
	// Just skip the one directory instead.
	return filepath.SkipDir
}

// http://www.webdav.org/specs/rfc4918.html#status.code.extensions.to.http11
const (
	StatusMulti = 207
)

func StatusText(code int) string {
	switch code {
	case StatusMulti:
		return "Multi-Status"
	}
	return http.StatusText(code)
}

var (
	errInvalidDepth      = errors.New("webdav: invalid depth")
	errInvalidPropfind   = errors.New("webdav: invalid propfind")
	errInvalidResponse   = errors.New("webdav: invalid response")
	errNoFileSystem      = errors.New("webdav: no file system")
	errUnsupportedMethod = errors.New("webdav: unsupported method")
)
