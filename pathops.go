package main

import (
	"strings"
)

func plen(s string) int {
	if s == "" {
		panic("empty path")
	} else if s == "." {
		return 0
	} else {
		return strings.Count(s, "/") + 1
	}
}

func pcut(s string, at int) (string, string) {
	if at < 0 {
		panic("negative argument")
	}
	if s == "." {
		s = ""
	}

	x := 0
	for range at {
		x++ // first byte of the component
		for x < len(s) && s[x] != '/' {
			x++ // subsequent non-slash bytes
		}
		if x < len(s) {
			x++ // terminal slash if any
		}
	}
	return ptrim(s[:x]), ptrim(s[x:])
}

func ptrim(s string) string {
	s = strings.Trim(s, "/")
	if s == "" {
		return "."
	} else {
		return s
	}
}

func pright(s string, i int) string {
	_, right := pcut(s, i)
	return right
}

func pleft(s string, i int) string {
	left, _ := pcut(s, i)
	return left
}

func pmid(s string, i, j int) string {
	return pright(pleft(s, j), i)
}
