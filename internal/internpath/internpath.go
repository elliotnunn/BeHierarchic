// Package internpath provides facilities for canonicalizing ("interning") paths.
// It pays special attention to saving memory when certain common filename prefix/suffixes are used.
package internpath

import (
	"strings"
	"unique"
)

// The canonical representation of a path.
// Satisfies the "comparable" interface, i.e. can be used as a map key or compared with "!=".
type Path struct {
	handle unique.Handle[path]
}

type path struct {
	dir  unique.Handle[path]
	base unique.Handle[string]
}

// Intern a path. It must satisfy [io/fs.ValidPath] or incorrect values will be returned by [Path.String] et al.
func New(name string) Path {
	var ret Path
	return ret.Join(name)
}

// String returns the path that was passed to [New]
func (p Path) String() string {
	var (
		strs                                []string
		length                              int
		handle                              = p.handle
		prependDotUnderscore, appendSpecial bool
	)

	for !isnil(handle) {
		structure := handle.Value()
		switch structure.base {
		case hasDotUnderscorePrefix:
			prependDotUnderscore = true
			handle = structure.dir
			continue
		case hasSpecialSuffix:
			appendSpecial = true
			handle = structure.dir
			continue
		}

		s := structure.base.Value()
		if prependDotUnderscore {
			s = "._" + s
		}
		if appendSpecial {
			s += "◆"
		}
		strs = append(strs, s)
		length += len(s)
		handle = structure.dir
		prependDotUnderscore, appendSpecial = false, false
	}
	if len(strs) == 0 {
		return "."
	}
	var b strings.Builder
	b.Grow(length + max(0, len(strs)-1))
	for i := len(strs) - 1; i >= 0; i-- {
		b.WriteString(strs[i])
		if i != 0 {
			b.WriteByte('/')
		}
	}
	return b.String()
}

// Base returns the filename, a performant shortcut for path.Base(p.String())
func (p Path) Base() string {
	var (
		handle                              = p.handle
		prependDotUnderscore, appendSpecial bool
	)

	for !isnil(handle) {
		structure := handle.Value()
		switch structure.base {
		case hasDotUnderscorePrefix:
			prependDotUnderscore = true
			handle = structure.dir
			continue
		case hasSpecialSuffix:
			appendSpecial = true
			handle = structure.dir
			continue
		}

		s := structure.base.Value()
		if prependDotUnderscore {
			s = "._" + s
		}
		if appendSpecial {
			s += "◆"
		}
		return s
	}
	return "."
}

// Dir returns the containing directory
func (p Path) Dir() Path {
	var (
		handle = p.handle
	)

	for !isnil(handle) {
		structure := handle.Value()
		handle = structure.dir
		switch structure.base {
		case hasDotUnderscorePrefix:
			continue
		case hasSpecialSuffix:
			continue
		}
		return Path{handle}
	}
	return New(".")
}

func (p Path) IsWithin(parent Path) bool {
	if isnil(p.handle) {
		return true
	}
	for {
		if p == parent {
			return true
		} else if isnil(p.handle) {
			return false
		} else {
			p = p.Dir()
		}
	}
}

// some special cases to save yet more RAM
var (
	hasDotUnderscorePrefix unique.Handle[string]
	hasSpecialSuffix       = unique.Make("/")
)

func isnil[T comparable](h unique.Handle[T]) bool {
	var zeroed unique.Handle[T]
	return h == zeroed
}

// Join adds more components to a path, a performant shortcut for New(path.Join(p.String(), name))
func (p Path) Join(name string) Path {
	if name == "." || name == "" {
		return p
	}
	for component := range strings.SplitSeq(name, "/") {
		if component == ".." {
			p = p.Dir()
			continue
		} else if component == "." {
			continue
		}

		var prependDotUnderscore, appendSpecial bool

		component, prependDotUnderscore = strings.CutPrefix(component, "._")
		component, appendSpecial = strings.CutSuffix(component, "◆")
		p = Path{unique.Make(path{dir: p.handle, base: unique.Make(component)})}
		if prependDotUnderscore {
			p = Path{unique.Make(path{dir: p.handle, base: hasDotUnderscorePrefix})}
		}
		if appendSpecial {
			p = Path{unique.Make(path{dir: p.handle, base: hasSpecialSuffix})}
		}
	}
	return p
}
