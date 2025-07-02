// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdavfs

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEscapeXML(t *testing.T) {
	// These test cases aren't exhaustive, and there is more than one way to
	// escape e.g. a quot (as "&#34;" or "&quot;") or an apos. We presume that
	// the encoding/xml package tests xml.EscapeText more thoroughly. This test
	// here is just a sanity check for this package's escapeXML function, and
	// its attempt to provide a fast path (and avoid a bytes.Buffer allocation)
	// when escaping filenames is obviously a no-op.
	testCases := map[string]string{
		"":              "",
		" ":             " ",
		"&":             "&amp;",
		"*":             "*",
		"+":             "+",
		",":             ",",
		"-":             "-",
		".":             ".",
		"/":             "/",
		"0":             "0",
		"9":             "9",
		":":             ":",
		"<":             "&lt;",
		">":             "&gt;",
		"A":             "A",
		"_":             "_",
		"a":             "a",
		"~":             "~",
		"\u0201":        "\u0201",
		"&amp;":         "&amp;amp;",
		"foo&<b/ar>baz": "foo&amp;&lt;b/ar&gt;baz",
	}

	for in, want := range testCases {
		if got := escapeXML(in); got != want {
			t.Errorf("in=%q: got %q, want %q", in, got, want)
		}
	}
}

func TestFilenameEscape(t *testing.T) {
	hrefRe := regexp.MustCompile(`<href>([^<]*)</href>`)
	displayNameRe := regexp.MustCompile(`<displayname>([^<]*)</displayname>`)
	do := func(method, urlStr string) (string, string, error) {
		req, err := http.NewRequest(method, urlStr, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Depth", "0")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", "", err
		}
		defer res.Body.Close()

		b, err := io.ReadAll(res.Body)
		if err != nil {
			return "", "", err
		}
		hrefMatch := hrefRe.FindStringSubmatch(string(b))
		if len(hrefMatch) != 2 {
			return "", "", errors.New("href not found")
		}
		displayNameMatch := displayNameRe.FindStringSubmatch(string(b))
		if len(displayNameMatch) != 2 {
			return "", "", errors.New("displayname not found")
		}

		return hrefMatch[1], displayNameMatch[1], nil
	}

	testCases := []struct {
		name, wantHref, wantDisplayName string
	}{{
		name:            `/foo%bar`,
		wantHref:        `/foo%25bar`,
		wantDisplayName: `foo%bar`,
	}, {
		name:            `/こんにちわ世界`,
		wantHref:        `/%E3%81%93%E3%82%93%E3%81%AB%E3%81%A1%E3%82%8F%E4%B8%96%E7%95%8C`,
		wantDisplayName: `こんにちわ世界`,
	}, {
		name:            `/Program Files/`,
		wantHref:        `/Program%20Files/`,
		wantDisplayName: `Program Files`,
	}, {
		name:            `/go+lang`,
		wantHref:        `/go+lang`,
		wantDisplayName: `go+lang`,
	}, {
		name:            `/go&lang`,
		wantHref:        `/go&amp;lang`,
		wantDisplayName: `go&amp;lang`,
	}, {
		name:            `/go<lang`,
		wantHref:        `/go%3Clang`,
		wantDisplayName: `go&lt;lang`,
	}, {
		name:            `/`,
		wantHref:        `/`,
		wantDisplayName: ``,
	}}

	fsys := make(fstest.MapFS)

	for _, tc := range testCases {
		if tc.name != "/" {
			if strings.HasSuffix(tc.name, "/") {
				fsys[strings.Trim(tc.name, "/")] = &fstest.MapFile{Mode: fs.ModeDir}
			} else {
				fsys[strings.Trim(tc.name, "/")] = &fstest.MapFile{Mode: 0}
			}
		}
	}

	srv := httptest.NewServer(&Handler{
		FS: fsys,
	})
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		u.Path = tc.name
		gotHref, gotDisplayName, err := do("PROPFIND", u.String())
		if err != nil {
			t.Errorf("name=%q: PROPFIND: %v", tc.name, err)
			continue
		}
		if gotHref != tc.wantHref {
			t.Errorf("name=%q: got href %q, want %q", tc.name, gotHref, tc.wantHref)
		}
		if gotDisplayName != tc.wantDisplayName {
			t.Errorf("name=%q: got dispayname %q, want %q", tc.name, gotDisplayName, tc.wantDisplayName)
		}
	}
}
