package internpath

import (
	"strings"
	"unique"
)

type Path struct {
	handle unique.Handle[path]
}

type path struct {
	dir  unique.Handle[path]
	base unique.Handle[string]
}

func New(name string) Path {
	var ret = roothandle
	if name != "." && name != "" {
		for component := range strings.SplitSeq(name, "/") {
			ret = unique.Make(path{dir: ret, base: unique.Make(component)})
		}
	}
	return Path{ret}
}

func (p Path) Base() string {
	return p.handle.Value().base.Value()
}

func (p Path) String() string {
	var strs []string
	var length int
	var handle = p.handle
	for handle != roothandle {
		structure := handle.Value()
		s := structure.base.Value()
		strs = append(strs, s)
		length += len(s)
		handle = structure.dir
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

var roothandle = unique.Make(path{base: unique.Make(".")})
