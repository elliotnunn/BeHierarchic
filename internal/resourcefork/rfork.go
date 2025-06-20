package resourcefork

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
)

type FS struct {
	AppleDouble io.ReaderAt
	once        sync.Once
	resData     uint32
	resTypeList uint32
	nType       uint16
}

// pattern is "TYPE/ID"
func (fsys *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}

	unitype, id, n := readPath(name)
	if n < 0 {
		return nil, fs.ErrNotExist
	} else if n == 0 {
		return &rootDir{fsys: fsys}, nil
	}

	fsys.once.Do(fsys.parse)

	t, nOfType, offsetOfType := fsys.typeLookup(unitype)
	if nOfType == 0 {
		return nil, fs.ErrNotExist
	}
	if n == 1 {
		return &typeDir{
			fsys:       fsys,
			t:          t,
			nOfType:    nOfType,
			typeOffset: offsetOfType,
		}, nil
	}

	offsetOfData := fsys.resourceLookup(id, nOfType, offsetOfType)
	if offsetOfData == 0 {
		return nil, fs.ErrNotExist
	}

	return &resourceFile{
		fsys:   fsys,
		id:     id,
		offset: offsetOfData,
	}, nil
}

func (fsys *FS) listTypes(list []fs.DirEntry, offset uint32) {
	fsys.once.Do(fsys.parse)
	tl := make([]byte, 8*len(list))
	_, err := fsys.AppleDouble.ReadAt(tl, int64(offset))
	if err != nil {
		return
	}

	for ; len(tl) > 0; tl = tl[8:] {
		list[0] = &typeDir{
			fsys:       fsys,
			t:          *(*[4]byte)(tl[:4]),
			nOfType:    binary.BigEndian.Uint16(tl[4:]) + 1,
			typeOffset: uint32(binary.BigEndian.Uint16(tl[6:])) + fsys.resTypeList,
		}
		list = list[1:]
	}
}

func (fsys *FS) listResources(offset uint32, n uint16) []fs.DirEntry {
	fsys.once.Do(fsys.parse)
	rl := make([]byte, 12*int(n))
	_, err := fsys.AppleDouble.ReadAt(rl, int64(offset))
	if err != nil {
		return nil
	}

	ret := make([]fs.DirEntry, 0, n)
	for ; len(rl) > 0; rl = rl[12:] {
		ret = append(ret, &resourceFile{
			fsys:   fsys,
			id:     int16(binary.BigEndian.Uint16(rl[0:])),
			offset: binary.BigEndian.Uint32(rl[4:])&0xffffff + fsys.resData,
		})
	}
	return ret
}

// Read the resource map, which is hopefully cached.
// TODO: try a binary search first
func (fsys *FS) typeLookup(unitype string) (t [4]byte, nOfType uint16, offsetOfType uint32) {
	fsys.once.Do(fsys.parse)
	tl := make([]byte, 8*int(fsys.nType))
	_, err := fsys.AppleDouble.ReadAt(tl, int64(fsys.resTypeList+2))
	if err != nil {
		return
	}

	for ; len(tl) > 0; tl = tl[8:] {
		t = *(*[4]byte)(tl[:4])
		if stringFromType(t) == unitype {
			nOfType = binary.BigEndian.Uint16(tl[4:]) + 1
			offsetOfType = uint32(binary.BigEndian.Uint16(tl[6:])) + fsys.resTypeList
			return
		}
	}
	return // failed the type lookup
}

func (fsys *FS) resourceLookup(id int16, nOfType uint16, offsetOfType uint32) (dataOffset uint32) {
	fsys.once.Do(fsys.parse)
	rl := make([]byte, 12*int(nOfType))
	_, err := fsys.AppleDouble.ReadAt(rl, int64(offsetOfType))
	if err != nil {
		return
	}

	for ; len(rl) > 0; rl = rl[12:] {
		if int16(binary.BigEndian.Uint16(rl[0:])) == id {
			dataOffset = binary.BigEndian.Uint32(rl[4:])&0xffffff + fsys.resData
			return
		}
	}
	return // failed the resource lookup
}

func (fsys *FS) mtime() time.Time {
	return time.Unix(0, 0) // TODO some real times
}

// Allows the resource fork to be AppleDouble-wrapped
func (fsys *FS) parse() {
	var forkOffset uint32
	var adHeader [26]byte
	_, err := fsys.AppleDouble.ReadAt(adHeader[:], 0)
	if err != nil {
		return
	} else if string(adHeader[:4]) == "\x00\x00\x01\x00" {
		forkOffset = 0 // bare resource fork
	} else if string(adHeader[:3]) == "\x00\x05\x16" {
		// AppleDouble
		nrec := binary.BigEndian.Uint16(adHeader[24:])
		recList := make([]byte, 12*int(nrec))
		_, err = fsys.AppleDouble.ReadAt(recList, 26)
		if err != nil {
			return
		}
		for ; len(recList) > 0; recList = recList[12:] {
			if binary.BigEndian.Uint32(recList) == 2 && binary.BigEndian.Uint32(recList[8:]) >= 286 {
				forkOffset = binary.BigEndian.Uint32(recList[4:])
				break // found resource fork record
			}

		}
		if len(recList) == 0 { // no resourcefork record
			return
		}
	} else {
		return // not a valid resource fork, so just be empty
	}

	var rfHeader [8]byte
	_, err = fsys.AppleDouble.ReadAt(rfHeader[:], int64(forkOffset))
	if err != nil {
		return
	}
	dataOffset := binary.BigEndian.Uint32(rfHeader[0:])
	if dataOffset != 256 {
		dump := make([]byte, 17*1024*1024)
		n, _ := fsys.AppleDouble.ReadAt(dump, 0)
		dump = dump[:n]
		os.WriteFile("/tmp/notrf", dump, 0o755)
		s, _ := appledouble.Dump(bytes.NewReader(dump))
		fmt.Println(s)
		panic("probably a corrupt file! logged at /tmp/notrf")
	}
	dataOffset += forkOffset
	mapOffset := binary.BigEndian.Uint32(rfHeader[4:]) + forkOffset

	var mapHeaderField [2]byte
	_, err = fsys.AppleDouble.ReadAt(mapHeaderField[:], int64(mapOffset+24))
	if err != nil {
		return
	}
	typeListOffset := uint32(binary.BigEndian.Uint16(mapHeaderField[0:])) + mapOffset

	var tlHeaderField [2]byte
	_, err = fsys.AppleDouble.ReadAt(tlHeaderField[:], int64(typeListOffset))
	if err != nil {
		return
	}
	typeCount := binary.BigEndian.Uint16(tlHeaderField[0:]) + 1

	// Setting these fields nonzero denotes success
	fsys.resData = dataOffset
	fsys.resTypeList = typeListOffset
	fsys.nType = typeCount
}

func readPath(p string) (t string, id int16, depth int) {
	const bad = -1
	if p == "." {
		return
	}
	split := strings.SplitN(p, "/", 3)
	depth = len(split)
	if depth < 1 || depth > 2 {
		depth = bad
		return
	}
	t = split[0]

	if depth < 2 {
		return
	}
	idInt, err := strconv.ParseInt(split[1], 10, 16)
	if err != nil {
		depth = bad
		return
	}
	id = int16(idInt)
	return
}

// Temporary, obviously needs a fix
func osType(s string) ([4]byte, bool) {
	s += "    "
	var ret [4]byte
	copy(ret[:], s)
	return ret, true
}
