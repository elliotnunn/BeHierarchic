package main

import (
	"fmt"
	"testing"
)

func TestPcut(t *testing.T) {
	cases := []struct {
		s    string
		i    int
		l, r string
	}{
		{".", -1, "panic", ""},
		{".", 0, ".", "."},
		{".", 1, "panic", ""},
		{"aaa", -1, "panic", ""},
		{"aaa", 0, ".", "aaa"},
		{"aaa", 1, "aaa", "."},
		{"aaa", 2, "panic", ""},
		{"aaa/bbb", -1, "panic", ""},
		{"aaa/bbb", 0, ".", "aaa/bbb"},
		{"aaa/bbb", 1, "aaa", "bbb"},
		{"aaa/bbb", 2, "aaa/bbb", "."},
		{"aaa/bbb", 3, "panic", ""},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("psplit(%q,%d)", c.s, c.i), func(t *testing.T) {
			if c.l == "panic" {
				defer func() {
					if recover() == nil {
						t.Errorf("Should have panicked but did not")
					}
				}()
			}

			l, r := pcut(c.s, c.i)
			if c.l != l || c.r != r {
				t.Errorf("Expected (%q, %q) but got (%q, %q)", c.l, c.r, l, r)
			}
		})
	}
}

func TestPmid(t *testing.T) {
	cases := []struct {
		s    string
		i, j int
		m    string
	}{
		{".", -1, -1, "panic"},
		{".", -1, 0, "panic"},
		{".", -1, 1, "panic"},
		{"aaa", -1, -1, "panic"},
		{"aaa", -1, 0, "panic"},
		{"aaa", -1, 1, "panic"},
		{"aaa", -1, 2, "panic"},
		{"aaa", 0, 2, "panic"},
		{"aaa", 1, 2, "panic"},
		{"aaa", 0, 0, "."},
		{"aaa", 1, 1, "."},
		{"aaa", 0, 1, "aaa"},
		{"aaa/bbb", 0, 0, "."},
		{"aaa/bbb", 0, 1, "aaa"},
		{"aaa/bbb", 0, 2, "aaa/bbb"},
		{"aaa/bbb", 1, 1, "."},
		{"aaa/bbb", 1, 2, "bbb"},
		{"aaa/bbb", 2, 2, "."},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("pmid(%q,%d,%d)", c.s, c.i, c.j), func(t *testing.T) {
			if c.m == "panic" {
				defer func() {
					if recover() == nil {
						t.Errorf("Should have panicked but did not")
					}
				}()
			}

			m := pmid(c.s, c.i, c.j)
			if c.m != m {
				t.Errorf("Expected %q but got %q", c.m, m)
			}
		})
	}
}
