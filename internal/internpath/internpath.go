// Package internpath provides facilities for canonicalizing ("interning") paths.
// The tradeoff for memory compactness is that paths, once created, are never freed.
package internpath

import (
	"fmt"
	"math/bits"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

/*
Internal Note
the structure of an entry in the large array is
    (my offset) - (offset of parent) : le128
    stringsize                       : le128
	basename                         : ascii
*/

// The canonical representation of a path.
// Satisfies the "comparable" interface, i.e. can be used as a map key or compared with "!=".
type Path struct{ offAndFlag uint32 }

func (p Path) offset() uint32 { return p.offAndFlag & (areaSize - 1) }
func (p Path) isZero() bool   { return p.offAndFlag == 0 }
func (p Path) vitals() (parent uint32, name string, ok bool) {
	if p.offset() == 0 {
		return 0, "", false
	}
	a := array[p.offset():]
	a, parent = get[uint32](a)
	a, length := get[int](a)
	parent = p.offset() - parent
	name = unsafe.String(&a[0], length)
	return parent, name, true
}

func (p *Path) setOffset(off uint32) { p.offAndFlag = p.offAndFlag&^(areaSize-1) | off&(areaSize-1) }
func (p *Path) setFlag(flag uint32, to bool) {
	p.offAndFlag &^= flag
	if to {
		p.offAndFlag |= flag
	}
}

var Root = Path{}

const (
	areaSize = 1 << 31
	// prependFlag    = 1 << 31
	// prependSpecial = "._"
)

var (
	mu    sync.RWMutex
	bump  uint32
	array *[areaSize]byte
)

func Stats() string {
	mu.RLock()
	defer mu.RUnlock()
	return fmt.Sprintf("%dMiB (blob=%d htab=%d htabFree=%d)",
		(int(bump)+4*len(htab))>>20,
		bump, 4*occupied, 4*(len(htab)-int(occupied)))
}

func MemoryUnknownToRuntime() int {
	return int(bump)
}

func init() { // make a large mapping to hide the enormous allocation from the Go runtime
	data, err := unix.Mmap(-1, 0, // fd and offset for an anonymous map
		areaSize,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		panic("mmap failed: " + err.Error())
	}
	array = (*[areaSize]byte)(data)

	// root entry
	putg(0) // offset
	putg(1) // len
	bump += uint32(copy(array[bump:], "."))
}

// New interns a path. It must satisfy [io/fs.ValidPath] or incorrect values will be returned by [Path.String] et al.
func New(name string) Path {
	path := Root
	path, _ = path.join(name, true)
	return path
}

// Get finds a path that has already been interned with [New].
func Get(name string) (path Path, ok bool) {
	path = Root
	return path.join(name, false)
}

// PutBase copies the filename into the supplied buffer and returns the length,
// or 0 if the buffer was too small.
func (p Path) PutBase(buf []byte) int {
	a := array[p.offset():]
	a, _ = get[uint64](a) // skip the offset field
	a, l := get[int](a)
	if l > len(buf) {
		return 0
	}
	return copy(buf, a[:l])
}

// PutBase copies the filename into the end of the supplied buffer and returns the length,
// or 0 if the buffer was too small.
func (p Path) PutBaseRight(buf []byte) int {
	a := array[p.offset():]
	a, _ = get[uint64](a) // skip the offset field
	a, l := get[int](a)
	if l > len(buf) {
		return 0
	}
	return copy(buf[len(buf)-l:], a[:l])
}

// PutBase copies the filename into the end of the supplied buffer and returns the length,
// or 0 if the buffer was too small.
func (p Path) BaseLen() int {
	a := array[p.offset():]
	a, _ = get[uint64](a) // skip the offset field
	_, l := get[int](a)
	return l
}

// Base returns the filename, a performant shortcut for path.Base(p.String())
func (p Path) Base() string {
	a := array[p.offset():]
	a, _ = get[uint64](a) // skip the offset field
	a, l := get[int](a)
	return unsafe.String(&a[0], l)
}

// Base returns the filename, a performant shortcut for path.Base(p.String())
func (p Path) String() string {
	if p == Root {
		return "."
	}
	accum := make([]byte, 16)
	n := 0
	slash := ""
	for comp := p; comp != Root; comp = comp.Dir() {
		shortfall := comp.BaseLen() + len(slash) + n - len(accum)
		if shortfall > 0 {
			newSize := max(16, 1<<bits.Len(uint(len(accum)+shortfall-1)))
			accum2 := make([]byte, newSize)
			copy(accum2[len(accum2)-n:], accum[len(accum)-n:])
			accum = accum2
		}
		n += copy(accum[len(accum)-n-len(slash):], slash)
		n += comp.PutBase(accum[len(accum)-n-comp.BaseLen():])
		slash = "/"
	}
	return unsafe.String(&accum[len(accum)-n], n)
}

// Dir returns the containing directory
//
// Taking the Dir of root will return root.
func (p Path) Dir() Path {
	_, offoff := get[uint32](array[p.offset():])
	p.setOffset(p.offset() - offoff)
	return p
}

func (p Path) IsWithin(parent Path) bool {
	if parent == Root {
		return true
	}
	for {
		if p == parent {
			return true
		} else if p == Root {
			return false
		} else {
			p = p.Dir()
		}
	}
}

// Join adds more components to a path, a performant shortcut for New(path.Join(p.String(), name))
func (p Path) Join(name string) Path {
	p, _ = p.join(name, true)
	return p
}

// TryJoin finds a path that has already been interned.
func (p Path) TryJoin(name string) (Path, bool) {
	return p.join(name, false)
}

func (p Path) join(name string, must bool) (Path, bool) {
	lockState := byte(0)
	defer func() {
		switch lockState {
		case 'r':
			mu.RUnlock()
		case 'w':
			mu.Unlock()
		}
	}()

	for component := range strings.SplitSeq(name, "/") {
		switch component {
		case "..":
			p = p.Dir()
		case "", ".":
			// go nowhere
		default:
			if lockState == 0 {
				mu.RLock()
				lockState = 'r'
			}

			var ok bool
			p, ok = singleTableOp(p.offset(), component, &lockState, must)
			if !ok {
				return Path{}, false
			}
		}
	}
	return p, true
}
