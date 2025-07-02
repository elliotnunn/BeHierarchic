// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdavfs

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
)

// Propstat describes a XML propstat element as defined in RFC 4918.
// See http://www.webdav.org/specs/rfc4918.html#ELEMENT_propstat
type Propstat struct {
	// Props contains the properties for which Status applies.
	Props []property

	// Status defines the HTTP status code of the properties in Prop.
	// Allowed values include, but are not limited to the WebDAV status
	// code extensions for HTTP/1.1.
	// http://www.webdav.org/specs/rfc4918.html#status.code.extensions.to.http11
	Status int

	// XMLError contains the XML representation of the optional error element.
	// XML content within this field must not rely on any predefined
	// namespace declarations or prefixes. If empty, the XML error element
	// is omitted.
	XMLError string

	// ResponseDescription contains the contents of the optional
	// responsedescription field. If empty, the XML element is omitted.
	ResponseDescription string
}

// makePropstats returns a slice containing those of x and y whose Props slice
// is non-empty. If both are empty, it returns a slice containing an otherwise
// zero Propstat whose HTTP status code is 200 OK.
func makePropstats(x, y Propstat) []Propstat {
	pstats := make([]Propstat, 0, 2)
	if len(x.Props) != 0 {
		pstats = append(pstats, x)
	}
	if len(y.Props) != 0 {
		pstats = append(pstats, y)
	}
	if len(pstats) == 0 {
		pstats = append(pstats, Propstat{
			Status: http.StatusOK,
		})
	}
	return pstats
}

// liveProps contains all supported, protected DAV: properties.
var liveProps = map[xml.Name]struct {
	// findFn implements the propfind function of this property. If nil,
	// it indicates a hidden property.
	findFn func(fs.FS, string, fs.FileInfo) (string, error)
	// dir is true if the property applies to directories.
	dir bool
}{
	{Space: "DAV:", Local: "resourcetype"}: {
		findFn: findResourceType,
		dir:    true,
	},
	{Space: "DAV:", Local: "displayname"}: {
		findFn: findDisplayName,
		dir:    true,
	},
	{Space: "DAV:", Local: "getcontentlength"}: {
		findFn: findContentLength,
		dir:    false,
	},
	{Space: "DAV:", Local: "getlastmodified"}: {
		findFn: findLastModified,
		// http://webdav.org/specs/rfc4918.html#PROPERTY_getlastmodified
		// suggests that getlastmodified should only apply to GETable
		// resources, and this package does not support GET on directories.
		//
		// Nonetheless, some WebDAV clients expect child directories to be
		// sortable by getlastmodified date, so this value is true, not false.
		// See golang.org/issue/15334.
		dir: true,
	},
	{Space: "DAV:", Local: "creationdate"}: {
		findFn: nil,
		dir:    false,
	},
	{Space: "DAV:", Local: "getcontentlanguage"}: {
		findFn: nil,
		dir:    false,
	},
	{Space: "DAV:", Local: "getcontenttype"}: {
		findFn: findContentType,
		dir:    false,
	},
	{Space: "DAV:", Local: "getetag"}: {
		findFn: findETag,
		// findETag implements ETag as the concatenated hex values of a file's
		// modification time and size. This is not a reliable synchronization
		// mechanism for directories, so we do not advertise getetag for DAV
		// collections.
		dir: false,
	},
}

// TODO(nigeltao) merge props and allprop?

// props returns the status of the properties named pnames for resource name.
//
// Each Propstat has a unique status and each property name will only be part
// of one Propstat element.
func props(fs fs.FS, name string, pnames []xml.Name) ([]Propstat, error) {
	f, err := fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	isDir := fi.IsDir()

	pstatOK := Propstat{Status: http.StatusOK}
	pstatNotFound := Propstat{Status: http.StatusNotFound}
	for _, pn := range pnames {
		// Must either be a live property or we don't know it.
		if prop := liveProps[pn]; prop.findFn != nil && (prop.dir || !isDir) {
			innerXML, err := prop.findFn(fs, name, fi)
			if err != nil {
				return nil, err
			}
			pstatOK.Props = append(pstatOK.Props, property{
				XMLName:  xml.Name{Local: pn.Local}, // strip extra "xmlns"
				InnerXML: []byte(innerXML),
			})
		} else {
			pstatNotFound.Props = append(pstatNotFound.Props, property{
				XMLName: xml.Name{Local: pn.Local}, // strip extra "xmlns"
			})
		}
	}
	return makePropstats(pstatOK, pstatNotFound), nil
}

// propnames returns the property names defined for resource name.
func propnames(fs fs.FS, name string) ([]xml.Name, error) {
	f, err := fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	isDir := fi.IsDir()

	pnames := make([]xml.Name, 0, len(liveProps))
	for pn, prop := range liveProps {
		if prop.findFn != nil && (prop.dir || !isDir) {
			pnames = append(pnames, pn)
		}
	}
	return pnames, nil
}

// allprop returns the properties defined for resource name and the properties
// named in include.
//
// Note that RFC 4918 defines 'allprop' to return the DAV: properties defined
// within the RFC plus dead properties. Other live properties should only be
// returned if they are named in 'include'.
//
// See http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
func allprop(fs fs.FS, name string, include []xml.Name) ([]Propstat, error) {
	pnames, err := propnames(fs, name)
	if err != nil {
		return nil, err
	}
	// Add names from include if they are not already covered in pnames.
	nameset := make(map[xml.Name]bool)
	for _, pn := range pnames {
		nameset[pn] = true
	}
	for _, pn := range include {
		if !nameset[pn] {
			pnames = append(pnames, pn)
		}
	}
	return props(fs, name, pnames)
}

func escapeXML(s string) string {
	for i := 0; i < len(s); i++ {
		// As an optimization, if s contains only ASCII letters, digits or a
		// few special characters, the escaped value is s itself and we don't
		// need to allocate a buffer and convert between string and []byte.
		switch c := s[i]; {
		case c == ' ' || c == '_' ||
			('+' <= c && c <= '9') || // Digits as well as + , - . and /
			('A' <= c && c <= 'Z') ||
			('a' <= c && c <= 'z'):
			continue
		}
		// Otherwise, go through the full escaping process.
		var buf bytes.Buffer
		xml.EscapeText(&buf, []byte(s))
		return buf.String()
	}
	return s
}

func findResourceType(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	if fi.IsDir() {
		return `<collection />`, nil
	}
	return "", nil
}

func findDisplayName(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	n := fi.Name()
	if n == "." {
		n = ""
	}
	return escapeXML(n), nil
}

func findContentLength(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	return strconv.FormatInt(fi.Size(), 10), nil
}

func findLastModified(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	return fi.ModTime().UTC().Format(http.TimeFormat), nil
}

func findContentType(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	return "application/octet-stream", nil
}

func findETag(fsys fs.FS, name string, fi os.FileInfo) (string, error) {
	// The Apache http 2.4 web server by default concatenates the
	// modification time and size of a file. We replicate the heuristic
	// with nanosecond granularity.
	return fmt.Sprintf(`"%x%x"`, fi.ModTime().UnixNano(), fi.Size()), nil
}
