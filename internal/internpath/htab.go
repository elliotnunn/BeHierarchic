package internpath

import (
	"fmt"
	"hash/maphash"
	"unsafe"
)

var htab = make([]Path, 1<<0)
var occupied uint32 = 0

// Upon entry, we are guaranteed to have either the read or the write lock
func singleTableOp(parent uint32, name string, haveWriteLock *bool, must bool) (Path, bool) {
	h := hashof(parent, name)
reProbeFromStart:
	for probe := uint32(h) & uint32(len(htab)-1); ; probe = (probe + 1) & uint32(len(htab)-1) {
	reProbeFromHere:
		if fparent, fname, ok := htab[probe].vitals(); ok && fparent == parent && fname == name {
			return htab[probe], true
		} else if htab[probe].isZero() {
			if !must {
				return Root, false
			}
			if !*haveWriteLock {
				oldTableSize := len(htab)
				mu.RUnlock()
				mu.Lock()
				*haveWriteLock = true
				if len(htab) != oldTableSize {
					goto reProbeFromStart // table grew
				}
				goto reProbeFromHere // table did not grow
			}
			newoff := bump
			putg(newoff - parent)
			putg(len(name))
			copy(array[bump:], name)
			bump += uint32(len(name))
			ret := Path{}
			ret.setOffset(newoff)
			htab[probe] = ret
			occupied++
			checkHash := ret.hash()
			if checkHash != h {
				panic("hash mismatch")
			}
			if int(occupied) >= len(htab)-len(htab)/16-len(htab)/8 { // 81.25% threshold
				grow()
			}
			return ret, true
		}
	}
}

func grow() {
	if len(htab) == 1<<32 {
		panic("hash table has grown too large")
	}
	oldtab := htab
	htab = make([]Path, 2*len(htab))
	for _, p := range oldtab {
		if p.isZero() {
			continue
		}
		h := p.hash()
		for probe := uint32(h) & uint32(len(htab)-1); ; probe = (probe + 1) & uint32(len(htab)-1) {
			if htab[probe].isZero() {
				htab[probe] = p
				break
			}
		}
	}
}

func dump() {
	fmt.Printf("occupancy = %d\n", uint64(occupied)*100/uint64(len(htab)))
	for i, p := range htab {
		parent, name, ok := p.vitals()
		if ok {
			fmt.Printf("%#x: offset=%#x, <parentoffset=%#x name=%q string=%q>, hash %#x\n",
				i, p.offset(), parent, name, p.String(), p.hash())
		} else {
			fmt.Printf("%#x: ---\n", i)
		}
	}
	fmt.Println()
}

var mh = maphash.MakeSeed()

func hashof(parent uint32, name string) uint64 {
	var hasher maphash.Hash
	hasher.SetSeed(mh)
	for range 4 {
		hasher.WriteByte(byte(parent))
		parent >>= 8
	}
	hasher.WriteString(name)
	return hasher.Sum64()
}

func (p Path) hash() uint64 {
	a := array[p.offset():]
	a, offoff := get[uint32](a) // skip the offset field
	a, length := get[int](a)
	return hashof(p.offset()-offoff, unsafe.String(&a[0], length))
}
