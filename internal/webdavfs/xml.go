// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdavfs

// The XML encoding is covered by Section 14.
// http://www.webdav.org/specs/rfc4918.html#xml.element.definitions

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

type countingReader struct {
	n int
	r io.Reader
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// next returns the next token, if any, in the XML stream of d.
// RFC 4918 requires to ignore comments, processing instructions
// and directives.
// http://www.webdav.org/specs/rfc4918.html#property_values
// http://www.webdav.org/specs/rfc4918.html#xml-extensibility
func next(d *xml.Decoder) (xml.Token, error) {
	for {
		t, err := d.Token()
		if err != nil {
			return t, err
		}
		switch t.(type) {
		case xml.Comment, xml.Directive, xml.ProcInst:
			continue
		default:
			return t, nil
		}
	}
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_prop (for propfind)
type propfindProps []xml.Name

// UnmarshalXML appends the property names enclosed within start to pn.
//
// It returns an error if start does not contain any properties or if
// properties contain values. Character data between properties is ignored.
func (pn *propfindProps) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		t, err := next(d)
		if err != nil {
			return err
		}
		switch t.(type) {
		case xml.EndElement:
			if len(*pn) == 0 {
				return fmt.Errorf("%s must not be empty", start.Name.Local)
			}
			return nil
		case xml.StartElement:
			name := t.(xml.StartElement).Name
			t, err = next(d)
			if err != nil {
				return err
			}
			if _, ok := t.(xml.EndElement); !ok {
				return fmt.Errorf("unexpected token %T", t)
			}
			*pn = append(*pn, xml.Name(name))
		}
	}
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propfind
type propfind struct {
	XMLName  xml.Name      `xml:"propfind"`
	Allprop  *struct{}     `xml:"allprop"`
	Propname *struct{}     `xml:"propname"`
	Prop     propfindProps `xml:"prop"`
	Include  propfindProps `xml:"include"`
}

func readPropfind(r io.Reader) (pf propfind, status int, err error) {
	c := countingReader{r: r}
	if err = xml.NewDecoder(&c).Decode(&pf); err != nil {
		if err == io.EOF {
			if c.n == 0 {
				// An empty body means to propfind allprop.
				// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
				return propfind{Allprop: new(struct{})}, 0, nil
			}
			err = errInvalidPropfind
		}
		return propfind{}, http.StatusBadRequest, err
	}

	if pf.Allprop == nil && pf.Include != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Allprop != nil && (pf.Prop != nil || pf.Propname != nil) {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Prop != nil && pf.Propname != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Propname == nil && pf.Allprop == nil && pf.Prop == nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	return pf, 0, nil
}

// property represents a single DAV resource property as defined in RFC 4918.
// See http://www.webdav.org/specs/rfc4918.html#data.model.for.resource.properties
type property struct {
	// XMLName is the fully qualified name that identifies this property.
	XMLName xml.Name

	// Lang is an optional xml:lang attribute.
	Lang string `xml:"xml:lang,attr,omitempty"`

	// InnerXML contains the XML representation of the property value.
	// See http://www.webdav.org/specs/rfc4918.html#property_values
	//
	// Property values of complex type or mixed-content must have fully
	// expanded XML namespaces or be self-contained with according
	// XML namespace declarations. They must not rely on any XML
	// namespace declarations within the scope of the XML document,
	// even including the DAV: namespace.
	InnerXML []byte `xml:",innerxml"`
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_error
type xmlError struct {
	XMLName  xml.Name `xml:"error"`
	InnerXML []byte   `xml:",innerxml"`
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propstat
type propstat struct {
	Prop                []property `xml:"prop>_ignored_"`
	Status              string     `xml:"status"`
	Error               *xmlError  `xml:"error"`
	ResponseDescription string     `xml:"responsedescription,omitempty"`
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_response
type response struct {
	XMLName             xml.Name   `xml:"response"`
	Href                []string   `xml:"href"`
	Propstat            []propstat `xml:"propstat"`
	Status              string     `xml:"status,omitempty"`
	Error               *xmlError  `xml:"error"`
	ResponseDescription string     `xml:"responsedescription,omitempty"`
}

// MultistatusWriter marshals one or more Responses into a XML
// multistatus response.
// See http://www.webdav.org/specs/rfc4918.html#ELEMENT_multistatus
type multistatusWriter struct {
	// ResponseDescription contains the optional responsedescription
	// of the multistatus XML element. Only the latest content before
	// close will be emitted. Empty response descriptions are not
	// written.
	responseDescription string

	w   http.ResponseWriter
	enc *xml.Encoder
}

// Write validates and emits a DAV response as part of a multistatus response
// element.
//
// It sets the HTTP status code of its underlying http.ResponseWriter to 207
// (Multi-Status) and populates the Content-Type header. If r is the
// first, valid response to be written, Write prepends the XML representation
// of r with a multistatus tag. Callers must call close after the last response
// has been written.
func (w *multistatusWriter) write(r *response) error {
	switch len(r.Href) {
	case 0:
		return errInvalidResponse
	case 1:
		if len(r.Propstat) > 0 != (r.Status == "") {
			return errInvalidResponse
		}
	default:
		if len(r.Propstat) > 0 || r.Status == "" {
			return errInvalidResponse
		}
	}
	err := w.writeHeader()
	if err != nil {
		return err
	}
	return w.enc.Encode(r)
}

// writeHeader writes a XML multistatus start element on w's underlying
// http.ResponseWriter and returns the result of the write operation.
// After the first write attempt, writeHeader becomes a no-op.
func (w *multistatusWriter) writeHeader() error {
	if w.enc != nil {
		return nil
	}
	w.w.Header().Add("Content-Type", "text/xml; charset=utf-8")
	w.w.WriteHeader(StatusMulti)
	_, err := fmt.Fprintf(w.w, `<?xml version="1.0" encoding="UTF-8"?>`)
	if err != nil {
		return err
	}
	w.enc = xml.NewEncoder(w.w)
	return w.enc.EncodeToken(xml.StartElement{
		Name: xml.Name{
			Space: "DAV:",
			Local: "multistatus",
		},
	})
}

// Close completes the marshalling of the multistatus response. It returns
// an error if the multistatus response could not be completed. If both the
// return value and field enc of w are nil, then no multistatus response has
// been written.
func (w *multistatusWriter) close() error {
	if w.enc == nil {
		return nil
	}
	var end []xml.Token
	if w.responseDescription != "" {
		name := xml.Name{Space: "DAV:", Local: "responsedescription"}
		end = append(end,
			xml.StartElement{Name: name},
			xml.CharData(w.responseDescription),
			xml.EndElement{Name: name},
		)
	}
	end = append(end, xml.EndElement{
		Name: xml.Name{Space: "DAV:", Local: "multistatus"},
	})
	for _, t := range end {
		err := w.enc.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return w.enc.Flush()
}
