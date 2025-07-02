// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdavfs

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestReadPropfind(t *testing.T) {
	testCases := []struct {
		desc       string
		input      string
		wantPF     propfind
		wantStatus int
	}{{
		desc: "propfind: propname",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:propname/>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName:  xml.Name{Space: "DAV:", Local: "propfind"},
			Propname: new(struct{}),
		},
	}, {
		desc:  "propfind: empty body means allprop",
		input: "",
		wantPF: propfind{
			Allprop: new(struct{}),
		},
	}, {
		desc: "propfind: allprop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"   <A:allprop/>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Allprop: new(struct{}),
		},
	}, {
		desc: "propfind: allprop followed by include",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:allprop/>\n" +
			"  <A:include><A:displayname/></A:include>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Allprop: new(struct{}),
			Include: propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: include followed by allprop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:include><A:displayname/></A:include>\n" +
			"  <A:allprop/>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Allprop: new(struct{}),
			Include: propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: propfind",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:displayname/></A:prop>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Prop:    propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: prop with ignored comments",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop>\n" +
			"    <!-- ignore -->\n" +
			"    <A:displayname><!-- ignore --></A:displayname>\n" +
			"  </A:prop>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Prop:    propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: propfind with ignored whitespace",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop>   <A:displayname/></A:prop>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Prop:    propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: propfind with ignored mixed-content",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop>foo<A:displayname/>bar</A:prop>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName: xml.Name{Space: "DAV:", Local: "propfind"},
			Prop:    propfindProps{xml.Name{Space: "DAV:", Local: "displayname"}},
		},
	}, {
		desc: "propfind: propname with ignored element (section A.4)",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:propname/>\n" +
			"  <E:leave-out xmlns:E='E:'>*boss*</E:leave-out>\n" +
			"</A:propfind>",
		wantPF: propfind{
			XMLName:  xml.Name{Space: "DAV:", Local: "propfind"},
			Propname: new(struct{}),
		},
	}, {
		desc:       "propfind: bad: junk",
		input:      "xxx",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: propname and allprop (section A.3)",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:propname/>" +
			"  <A:allprop/>" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: propname and prop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:displayname/></A:prop>\n" +
			"  <A:propname/>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: allprop and prop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:allprop/>\n" +
			"  <A:prop><A:foo/><A:/prop>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: empty propfind with ignored element (section A.4)",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <E:expired-props/>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: empty prop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop/>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: prop with just chardata",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop>foo</A:prop>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "bad: interrupted prop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:foo></A:prop>\n",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "bad: malformed end element prop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:foo/></A:bar></A:prop>\n",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: property with chardata value",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:foo>bar</A:foo></A:prop>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: property with whitespace value",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:prop><A:foo> </A:foo></A:prop>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}, {
		desc: "propfind: bad: include without allprop",
		input: "" +
			"<A:propfind xmlns:A='DAV:'>\n" +
			"  <A:include><A:foo/></A:include>\n" +
			"</A:propfind>",
		wantStatus: http.StatusBadRequest,
	}}

	for _, tc := range testCases {
		pf, status, err := readPropfind(strings.NewReader(tc.input))
		if tc.wantStatus != 0 {
			if err == nil {
				t.Errorf("%s: got nil error, want non-nil", tc.desc)
				continue
			}
		} else if err != nil {
			t.Errorf("%s: %v", tc.desc, err)
			continue
		}
		if !reflect.DeepEqual(pf, tc.wantPF) || status != tc.wantStatus {
			t.Errorf("%s:\ngot  propfind=%v, status=%v\nwant propfind=%v, status=%v",
				tc.desc, pf, status, tc.wantPF, tc.wantStatus)
			continue
		}
	}
}

func TestMultistatusWriter(t *testing.T) {
	///The "section x.y.z" test cases come from section x.y.z of the spec at
	// http://www.webdav.org/specs/rfc4918.html
	testCases := []struct {
		desc        string
		responses   []response
		respdesc    string
		writeHeader bool
		wantXML     string
		wantCode    int
		wantErr     error
	}{{
		desc: "section 9.2.2 (failed dependency)",
		responses: []response{{
			Href: []string{"http://example.com/foo"},
			Propstat: []propstat{{
				Prop: []property{{
					XMLName: xml.Name{
						Space: "http://ns.example.com/",
						Local: "Authors",
					},
				}},
				Status: "HTTP/1.1 424 Failed Dependency",
			}, {
				Prop: []property{{
					XMLName: xml.Name{
						Space: "http://ns.example.com/",
						Local: "Copyright-Owner",
					},
				}},
				Status: "HTTP/1.1 409 Conflict",
			}},
			ResponseDescription: "Copyright Owner cannot be deleted or altered.",
		}},
		wantXML: `` +
			`<?xml version="1.0" encoding="UTF-8"?>` +
			`<multistatus xmlns="DAV:">` +
			`  <response>` +
			`    <href>http://example.com/foo</href>` +
			`    <propstat>` +
			`      <prop>` +
			`        <Authors xmlns="http://ns.example.com/"></Authors>` +
			`      </prop>` +
			`      <status>HTTP/1.1 424 Failed Dependency</status>` +
			`    </propstat>` +
			`    <propstat xmlns="DAV:">` +
			`      <prop>` +
			`        <Copyright-Owner xmlns="http://ns.example.com/"></Copyright-Owner>` +
			`      </prop>` +
			`      <status>HTTP/1.1 409 Conflict</status>` +
			`    </propstat>` +
			`  <responsedescription>Copyright Owner cannot be deleted or altered.</responsedescription>` +
			`</response>` +
			`</multistatus>`,
		wantCode: StatusMulti,
	}, {
		desc: "section 9.6.2 (lock-token-submitted)",
		responses: []response{{
			Href:   []string{"http://example.com/foo"},
			Status: "HTTP/1.1 423 Locked",
			Error: &xmlError{
				InnerXML: []byte(`<lock-token-submitted xmlns="DAV:"/>`),
			},
		}},
		wantXML: `` +
			`<?xml version="1.0" encoding="UTF-8"?>` +
			`<multistatus xmlns="DAV:">` +
			`  <response>` +
			`    <href>http://example.com/foo</href>` +
			`    <status>HTTP/1.1 423 Locked</status>` +
			`    <error><lock-token-submitted xmlns="DAV:"/></error>` +
			`  </response>` +
			`</multistatus>`,
		wantCode: StatusMulti,
	}, {
		desc: "section 9.1.3",
		responses: []response{{
			Href: []string{"http://example.com/foo"},
			Propstat: []propstat{{
				Prop: []property{{
					XMLName: xml.Name{Space: "http://ns.example.com/boxschema/", Local: "bigbox"},
					InnerXML: []byte(`` +
						`<BoxType xmlns="http://ns.example.com/boxschema/">` +
						`Box type A` +
						`</BoxType>`),
				}, {
					XMLName: xml.Name{Space: "http://ns.example.com/boxschema/", Local: "author"},
					InnerXML: []byte(`` +
						`<Name xmlns="http://ns.example.com/boxschema/">` +
						`J.J. Johnson` +
						`</Name>`),
				}},
				Status: "HTTP/1.1 200 OK",
			}, {
				Prop: []property{{
					XMLName: xml.Name{Space: "http://ns.example.com/boxschema/", Local: "DingALing"},
				}, {
					XMLName: xml.Name{Space: "http://ns.example.com/boxschema/", Local: "Random"},
				}},
				Status:              "HTTP/1.1 403 Forbidden",
				ResponseDescription: "The user does not have access to the DingALing property.",
			}},
		}},
		respdesc: "There has been an access violation error.",
		wantXML: `` +
			`<?xml version="1.0" encoding="UTF-8"?>` +
			`<multistatus xmlns="DAV:" xmlns:B="http://ns.example.com/boxschema/">` +
			`  <response>` +
			`    <href>http://example.com/foo</href>` +
			`    <propstat>` +
			`      <prop>` +
			`        <B:bigbox><B:BoxType>Box type A</B:BoxType></B:bigbox>` +
			`        <B:author><B:Name>J.J. Johnson</B:Name></B:author>` +
			`      </prop>` +
			`      <status>HTTP/1.1 200 OK</status>` +
			`    </propstat>` +
			`    <propstat>` +
			`      <prop>` +
			`        <B:DingALing/>` +
			`        <B:Random/>` +
			`      </prop>` +
			`      <status>HTTP/1.1 403 Forbidden</status>` +
			`      <responsedescription>The user does not have access to the DingALing property.</responsedescription>` +
			`    </propstat>` +
			`  </response>` +
			`  <responsedescription>There has been an access violation error.</responsedescription>` +
			`</multistatus>`,
		wantCode: StatusMulti,
	}, {
		desc: "no response written",
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc:     "no response written (with description)",
		respdesc: "too bad",
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc:        "empty multistatus with header",
		writeHeader: true,
		wantXML:     `<multistatus xmlns="DAV:"></multistatus>`,
		wantCode:    StatusMulti,
	}, {
		desc: "bad: no href",
		responses: []response{{
			Propstat: []propstat{{
				Prop: []property{{
					XMLName: xml.Name{
						Space: "http://example.com/",
						Local: "foo",
					},
				}},
				Status: "HTTP/1.1 200 OK",
			}},
		}},
		wantErr: errInvalidResponse,
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc: "bad: multiple hrefs and no status",
		responses: []response{{
			Href: []string{"http://example.com/foo", "http://example.com/bar"},
		}},
		wantErr: errInvalidResponse,
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc: "bad: one href and no propstat",
		responses: []response{{
			Href: []string{"http://example.com/foo"},
		}},
		wantErr: errInvalidResponse,
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc: "bad: status with one href and propstat",
		responses: []response{{
			Href: []string{"http://example.com/foo"},
			Propstat: []propstat{{
				Prop: []property{{
					XMLName: xml.Name{
						Space: "http://example.com/",
						Local: "foo",
					},
				}},
				Status: "HTTP/1.1 200 OK",
			}},
			Status: "HTTP/1.1 200 OK",
		}},
		wantErr: errInvalidResponse,
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}, {
		desc: "bad: multiple hrefs and propstat",
		responses: []response{{
			Href: []string{
				"http://example.com/foo",
				"http://example.com/bar",
			},
			Propstat: []propstat{{
				Prop: []property{{
					XMLName: xml.Name{
						Space: "http://example.com/",
						Local: "foo",
					},
				}},
				Status: "HTTP/1.1 200 OK",
			}},
		}},
		wantErr: errInvalidResponse,
		// default of http.responseWriter
		wantCode: http.StatusOK,
	}}

	n := xmlNormalizer{omitWhitespace: true}
loop:
	for _, tc := range testCases {
		rec := httptest.NewRecorder()
		w := multistatusWriter{w: rec, responseDescription: tc.respdesc}
		if tc.writeHeader {
			if err := w.writeHeader(); err != nil {
				t.Errorf("%s: got writeHeader error %v, want nil", tc.desc, err)
				continue
			}
		}
		for _, r := range tc.responses {
			if err := w.write(&r); err != nil {
				if err != tc.wantErr {
					t.Errorf("%s: got write error %v, want %v",
						tc.desc, err, tc.wantErr)
				}
				continue loop
			}
		}
		if err := w.close(); err != tc.wantErr {
			t.Errorf("%s: got close error %v, want %v",
				tc.desc, err, tc.wantErr)
			continue
		}
		if rec.Code != tc.wantCode {
			t.Errorf("%s: got HTTP status code %d, want %d\n",
				tc.desc, rec.Code, tc.wantCode)
			continue
		}
		gotXML := rec.Body.String()
		eq, err := n.equalXML(strings.NewReader(gotXML), strings.NewReader(tc.wantXML))
		if err != nil {
			t.Errorf("%s: equalXML: %v", tc.desc, err)
			continue
		}
		if !eq {
			t.Errorf("%s: XML body\ngot  %s\nwant %s", tc.desc, gotXML, tc.wantXML)
		}
	}
}

// xmlNormalizer normalizes XML.
type xmlNormalizer struct {
	// omitWhitespace instructs to ignore whitespace between element tags.
	omitWhitespace bool
	// omitComments instructs to ignore XML comments.
	omitComments bool
}

// normalize writes the normalized XML content of r to w. It applies the
// following rules
//
//   - Rename namespace prefixes according to an internal heuristic.
//   - Remove unnecessary namespace declarations.
//   - Sort attributes in XML start elements in lexical order of their
//     fully qualified name.
//   - Remove XML directives and processing instructions.
//   - Remove CDATA between XML tags that only contains whitespace, if
//     instructed to do so.
//   - Remove comments, if instructed to do so.
func (n *xmlNormalizer) normalize(w io.Writer, r io.Reader) error {
	d := xml.NewDecoder(r)
	e := xml.NewEncoder(w)
	for {
		t, err := d.Token()
		if err != nil {
			if t == nil && err == io.EOF {
				break
			}
			return err
		}
		switch val := t.(type) {
		case xml.Directive, xml.ProcInst:
			continue
		case xml.Comment:
			if n.omitComments {
				continue
			}
		case xml.CharData:
			if n.omitWhitespace && len(bytes.TrimSpace(val)) == 0 {
				continue
			}
		case xml.StartElement:
			start, _ := xml.CopyToken(val).(xml.StartElement)
			attr := start.Attr[:0]
			for _, a := range start.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				attr = append(attr, a)
			}
			sort.Sort(byName(attr))
			start.Attr = attr
			t = start
		}
		err = e.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return e.Flush()
}

// equalXML tests for equality of the normalized XML contents of a and b.
func (n *xmlNormalizer) equalXML(a, b io.Reader) (bool, error) {
	var buf bytes.Buffer
	if err := n.normalize(&buf, a); err != nil {
		return false, err
	}
	normA := buf.String()
	buf.Reset()
	if err := n.normalize(&buf, b); err != nil {
		return false, err
	}
	normB := buf.String()
	return normA == normB, nil
}

type byName []xml.Attr

func (a byName) Len() int      { return len(a) }
func (a byName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool {
	if a[i].Name.Space != a[j].Name.Space {
		return a[i].Name.Space < a[j].Name.Space
	}
	return a[i].Name.Local < a[j].Name.Local
}
