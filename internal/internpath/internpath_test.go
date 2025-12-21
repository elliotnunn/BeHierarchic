package internpath

import (
	"fmt"
	"math/rand/v2"
	"path"
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

func TestUnique(t *testing.T) {
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

	var firsttry []Path
	for _, p := range cases {
		firsttry = append(firsttry, New(p))
	}

	for i, p := range cases {
		t.Run(p, func(t *testing.T) {
			wantobj := firsttry[i]
			gotobj := New(p)
			if wantobj != gotobj {
				t.Errorf("%s != %s", wantobj, gotobj)
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

func TestPara(t *testing.T) {
	fnames := []string{
		"a/b/c/d/e/f",
		"a/b/c/d/e/ff",
		"a/b/c/9999",
		"a/b/c/d/e/f",
		"a/b/c/d/e/ff",
		"a/b/c/9999",
		"a/b/c/d/e/f",
		"a/b/c/d/e/ff",
		"a/b/c/9999",
		"a/b/c/d/e/f",
		"a/b/c/d/e/ff",
		"a/b/c/9999",
	}
	for range 100 {
		for _, n := range fnames {
			t.Run(n, func(t *testing.T) {
				t.Parallel()
				_ = New(n)
			})
		}
	}
}

func TestLarge(t *testing.T) {
	var paths []string
	rnd := rand.NewPCG('e', 'n')
	for range 1000000 {
		s := fmt.Sprintf("%016x%016x", rnd.Uint64(), rnd.Uint64())
		s = strings.ReplaceAll(s, "5", "/")
		for strings.Contains(s, "//") {
			s = strings.ReplaceAll(s, "//", "/")
		}
		s = strings.Trim(s, "/")
		for ; s != "."; s = path.Dir(s) {
			paths = append(paths, s)
		}
	}

	vals := make(map[string]Path)
	for _, s := range paths {
		nu := New(s)
		prev, ok := vals[s]
		if ok && prev != nu {
			t.Errorf("multiple pathobjects for %q: offset=%#x offset=%#x", s, nu.offset(), prev.offset())
		}
		vals[s] = nu
	}

	for s, v := range vals {
		nu := New(s)
		if nu != v {
			t.Errorf("failed to reproduce the same pathobject for %s: got %v want %v", s, nu, v)
		}
	}
	t.Log(Stats())
}
