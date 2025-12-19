package internpath

import (
	gopath "path"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []string{
		".",
		"a/b",
		"a/b/c",
		"a◆/b",
		"a/b◆",
		"a◆/b◆",
		"._a/b",
		"a/._b",
		"._a/._b",
		"._a◆/b",
		"a/._b◆",
		"._a/b◆",
	}

	for _, want := range cases {
		t.Run(want, func(t *testing.T) {
			got := New(want).String()
			if got != want {
				t.Errorf("New(%q).String(): wanted %q, got %q", want, want, got)
			}

			gotbuf := make([]byte, 128)
			n := New(want).PutBase(gotbuf)
			if string(gotbuf[:n]) != New(want).Base() {
				t.Errorf("New(%q).Append(...): wanted %q, got %q", want, New(want).Base(), string(gotbuf[:n]))
			}

			gotbase := New(want).Base()
			wantbase := gopath.Base(got)
			if gotbase != wantbase {
				t.Errorf("New(%q).Base(): wanted %q, got %q", want, wantbase, gotbase)
			}

			gotdir := New(want).Dir().String()
			wantdir := gopath.Dir(got)
			if gotdir != wantdir {
				t.Errorf("New(%q).Dir().String(): wanted %q, got %q", want, wantdir, gotdir)
			}
		})
	}
}

func TestJoin(t *testing.T) {
	cases := []string{
		".+.",
		".+a",
		"a+.",
		"a+..",
		"a/b+../c",
	}

	for _, tcase := range cases {
		t.Run(tcase, func(t *testing.T) {
			first, last, _ := strings.Cut(tcase, "+")

			want := gopath.Join(first, last)
			got := New(first).Join(last).String()

			if got != want {
				t.Errorf("New(%q).Join(%q).String(): wanted %q, got %q", first, last, want, got)
			}
		})
	}
}
