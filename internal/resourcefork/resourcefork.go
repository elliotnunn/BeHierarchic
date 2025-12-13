// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package resourcefork

import (
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/sectionreader"
)

var ErrFormat = errors.New("not a valid resource fork")

// New opens a resource fork
func New(r io.ReaderAt) (fs.FS, error) {
	return New2(r, r)
}

// New2 routes headers and data requests through different readers, to help exotic caching schemes
func New2(headerReader, dataReader io.ReaderAt) (fs.FS, error) {
	forkOffset := resourceForkOffset(headerReader) // AppleDouble

	var rfHeader [16]byte
	n, err := headerReader.ReadAt(rfHeader[:], forkOffset)
	if n != len(rfHeader) {
		return nil, err
	}
	if binary.BigEndian.Uint32(rfHeader[0:]) != 256 {
		return nil, ErrFormat
	}
	dataOffset := forkOffset + int64(binary.BigEndian.Uint32(rfHeader[0:]))
	mapOffset := forkOffset + int64(binary.BigEndian.Uint32(rfHeader[4:]))
	dataSize := int64(binary.BigEndian.Uint32(rfHeader[8:]))
	mapSize := int64(binary.BigEndian.Uint32(rfHeader[12:]))

	rmap := make([]byte, mapSize)
	n, err = headerReader.ReadAt(rmap, mapOffset)
	if n != len(rmap) {
		return nil, err
	}

	tlo := int(binary.BigEndian.Uint16(rmap[24:]))
	nlo := int(binary.BigEndian.Uint16(rmap[26:]))
	if len(rmap) < tlo+2 || len(rmap) < nlo {
		return nil, ErrFormat
	}
	typeList := rmap[tlo:]
	nameList := rmap[nlo:]

	type r struct {
		offset int64
		te     []byte
		re     []byte
		ne     []byte
	}
	var rlist []r

	nType := int(binary.BigEndian.Uint16(typeList[0:]) + 1)
	if len(typeList) < 2+8*nType {
		return nil, ErrFormat
	}
	for i := range nType {
		te := typeList[2+8*i:][:8]
		nRes := int(binary.BigEndian.Uint16(te[4:]) + 1)
		sf := int(binary.BigEndian.Uint16(te[6:]))
		if len(typeList) < sf+12*nRes {
			return nil, ErrFormat
		}
		for j := range nRes {
			re := typeList[sf+12*j:][:12]
			// id :=
			nameof := int(int16(binary.BigEndian.Uint16(re[2:])))
			var ne []byte
			if nameof >= 0 {
				if len(nameList) < nameof+1 {
					return nil, ErrFormat
				}
				ne = nameList[nameof:]
			}
			dataof := dataOffset + int64(binary.BigEndian.Uint32(re[4:])&0xffffff) + 4 // the critical field
			if dataOffset+dataSize < dataof {
				return nil, ErrFormat
			}
			rlist = append(rlist, r{offset: dataof, te: te, re: re, ne: ne})
		}
	}

	slices.SortFunc(rlist, func(a, b r) int { return cmp.Compare(a.offset, b.offset) })

	fsys := fskeleton.New()
	defer fsys.NoMore()
	for _, r := range rlist {
		var se [4]byte
		n, err = headerReader.ReadAt(se[:], r.offset-4)
		if n != len(se) {
			return nil, err
		}
		size := int64(binary.BigEndian.Uint32(se[:]))

		path1 := filenameFrom(r.te[:4])
		path2 := path1 + "/" + strconv.Itoa(int(int16(binary.BigEndian.Uint16(r.re[0:]))))
		path3 := ""
		if len(r.ne) > 0 {
			nlen := int(r.ne[0])
			if len(r.ne) < 1+nlen {
				return nil, ErrFormat
			}
			path3 = path1 + "/named/" + filenameFrom(r.ne[1:][:nlen])
		}

		fsys.CreateReaderAt(path2, r.offset, sectionreader.Section(dataReader, r.offset, size), size, 0, time.Time{})
		if path3 != "" {
			fsys.Symlink(path3, 0, path2, 0, time.Time{})
		}
	}
	return fsys, nil
}

func resourceForkOffset(r io.ReaderAt) int64 {
	header := make([]byte, 3)
	n, _ := r.ReadAt(header, 0)
	if n < len(header) {
		return 0
	}
	if string(header) != "\x00\x05\x16" {
		return 0
	}
	nf := make([]byte, 2)
	n, _ = r.ReadAt(nf, 24)
	if n != len(nf) {
		return 0
	}
	recList := make([]byte, 12*int(binary.BigEndian.Uint32(nf)))
	n, _ = r.ReadAt(recList, 26)
	if n != len(recList) {
		return 0
	}
	for ; len(recList) > 0; recList = recList[12:] {
		if binary.BigEndian.Uint32(recList) == 2 && binary.BigEndian.Uint32(recList[8:]) >= 286 {
			return int64(binary.BigEndian.Uint32(recList[4:]))
		}
	}
	return 0
}
