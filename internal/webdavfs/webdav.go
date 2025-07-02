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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
		return http.StatusMethodNotAllowed, nil
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
	var skipResp error = nil
	if info != nil && info.IsDir() {
		skipResp = filepath.SkipDir
	}

	if errors.Is(err, os.ErrPermission) {
		// If the server cannot recurse into a directory because it is not allowed,
		// then there is nothing more to say about it. Just skip sending anything.
		return skipResp
	}

	if _, ok := err.(*os.PathError); ok {
		// If the file is just bad, it couldn't be a proper WebDAV resource. Skip it.
		return skipResp
	}

	// We need to be careful with other errors: there is no way to abort the xml stream
	// part way through while returning a valid PROPFIND response. Returning only half
	// the data would be misleading, but so would be returning results tainted by errors.
	// The current behaviour by returning an error here leads to the stream being aborted,
	// and the parent http server complaining about writing a spurious header. We should
	// consider further enhancing this error handling to more gracefully fail, or perhaps
	// buffer the entire response until we've walked the tree.
	return err
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
