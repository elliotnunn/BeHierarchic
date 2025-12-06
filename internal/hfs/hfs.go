// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Implement fs.FS for Apple's very old Hierarchical File System
// (plain HFS from 1985, not to be confused with HFS+)
package hfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/multireaderat"
	"github.com/elliotnunn/BeHierarchic/internal/sectionreader"
)

// New opens an HFS image
func New(r io.ReaderAt) (fs.FS, error) {
	return New2(r, r)
}

// New2 routes headers and data requests through different readers, to help exotic caching schemes
func New2(headerReader, dataReader io.ReaderAt) (retfs fs.FS, reterr error) {
	var mdb [512]byte
	_, err := headerReader.ReadAt(mdb[:], 0x400)
	if err != nil {
		return nil, fmt.Errorf("HFS Master Directory Block unreadable: %w", err)
	}

	if mdb[0] != 'B' || mdb[1] != 'D' {
		return nil, errors.New("HFS magic number absent")
	}

	drNmAlBlks := binary.BigEndian.Uint16(mdb[0x12:])
	drAlBlkSiz := binary.BigEndian.Uint32(mdb[0x14:])
	drAlBlSt := binary.BigEndian.Uint16(mdb[0x1c:])

	// There is a compression format that deceptively leaves the magic number intact.
	// Attempt to detect this early by checking the image size.
	// Don't resort to an actual read, because the seek might be expensive.
	minSize := int64(drAlBlSt)*512 + int64(drAlBlkSiz)*int64(drNmAlBlks)
	if actualSize, ok := tryGetSizeCheaply(headerReader); ok {
		if actualSize < minSize {
			return nil, fmt.Errorf("likely Disk Copy compressed HFS image: expected %db but got %db", minSize, actualSize)
		}
	}

	overflowTree, err := parseBTree(
		newAccumReader(
			parseExtents(mdb[0x86:]).
				toBytes(drAlBlkSiz, drAlBlSt).
				makeReader(headerReader)))
	if err != nil {
		return nil, fmt.Errorf("extents overflow file: %w", err)
	}
	overflow := parseExtentsOverflow(overflowTree)

	catalog, err := parseBTree(
		newAccumReader(
			parseExtents(mdb[0x96:]).
				chaseOverflow(overflow, 4, false).
				toBytes(drAlBlkSiz, drAlBlSt).
				makeReader(headerReader)))
	if err != nil {
		return nil, fmt.Errorf("catalog file: %w", err)
	}

	dirs := dirPaths(catalog)
	fsys := fskeleton.New()
	defer fsys.NoMore()

	// Make sure fskeleton finds out about forks in the order that they exist on disk
	// (and hope for no fragmented files)
	deferred := make(map[int64]func())

	for _, rec := range catalog {
		key, val := rec.Key(), rec.Val()
		parent := binary.BigEndian.Uint32(key[1:])
		switch val[0] {
		case 1: // dir
			cnid := binary.BigEndian.Uint32(val[6:])

			var meta appledouble.AppleDouble
			meta.LoadDInfo((*[16]byte)(val[0x16:]))
			meta.CreateTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0xa:]))
			meta.ModTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0xe:]))
			meta.BkTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0x12:]))
			meta.Locked = val[3]&1 != 0
			adRead, adSize := meta.ForDir()

			fsys.CreateDir(dirs[cnid], fs.FileMode(0), meta.ModTime, ino(cnid))
			fsys.CreateReaderFile(dirs[cnid], -2*int64(cnid) /*unorderable*/, adRead, adSize, 0, meta.ModTime, ino(cnid))

		case 2: // file
			cnid := binary.BigEndian.Uint32(val[0x14:])
			name := path.Join(dirs[parent], strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":"))

			var meta appledouble.AppleDouble
			meta.LoadFInfo((*[16]byte)(val[4:]))
			meta.CreateTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0x2c:]))
			meta.ModTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0x30:]))
			meta.BkTime = appledouble.MacTime(binary.BigEndian.Uint32(val[0x34:]))
			meta.Locked = val[3]&1 != 0

			dfSize, rfSize := int64(binary.BigEndian.Uint32(val[0x1a:])), int64(binary.BigEndian.Uint32(val[0x24:]))
			dfExtents := parseExtents(val[0x4a:]).
				chaseOverflow(overflow, cnid, false).
				toBytes(drAlBlkSiz, drAlBlSt).
				clipExtents(dfSize)
			rfExtents := parseExtents(val[0x56:]).
				chaseOverflow(overflow, cnid, true).
				toBytes(drAlBlkSiz, drAlBlSt).
				clipExtents(int64(rfSize))
			dfReader, rfReader := dfExtents.makeReader(dataReader), rfExtents.makeReader(dataReader)

			adReader, adSize := meta.WithResourceFork(rfReader, rfSize)

			dfLoc, rfLoc := -2*int64(cnid)-1, -2*int64(cnid) // unorderable
			if len(dfExtents) > 0 {
				dfLoc = dfExtents[0]
			}
			if len(rfExtents) > 0 {
				rfLoc = rfExtents[0]
			}

			deferred[dfLoc] = func() {
				fsys.CreateReaderAtFile(name, dfLoc, dfReader, dfSize, 0, meta.ModTime, ino(cnid))
			}
			deferred[rfLoc] = func() {
				fsys.CreateReaderAtFile(appledouble.Sidecar(name), rfLoc, adReader, adSize, 0, meta.ModTime, ino(cnid))
			}
		}
	}

	for _, offset := range slices.Sorted(maps.Keys(deferred)) {
		deferred[offset]()
	}
	return fsys, nil
}

// For chasing through the extents overflow file
type extKey struct {
	cnid  uint32
	n     uint16
	isres bool
}

// So these types can be used as a method receiver, enabling.method.chains
type blockExtents []uint16 // alternating (firstblock, blockcount)
type byteExtents []int64   // alternating (firstbyte, bytecount)
type bRecord []byte

func (x byteExtents) makeReader(fs io.ReaderAt) multireaderat.SizeReaderAt {
	subreaders := make([]multireaderat.SizeReaderAt, 0, len(x)/2)
	for i := 0; i < len(x); i += 2 {
		xstart, xlen := x[i], x[i+1]
		subreaders = append(subreaders, sectionreader.Section(fs, xstart, xlen))
	}
	return multireaderat.New(subreaders...)
}

func (c bRecord) Val() []byte { return c[(int(c[0])+2)&^1:] }
func (c bRecord) Key() []byte { return c[1:][:c[0]] }

func parseExtents(record []byte) blockExtents {
	var extents blockExtents
	for i := 0; i < 12; i += 4 {
		start := binary.BigEndian.Uint16(record[i:])
		count := binary.BigEndian.Uint16(record[i+2:])
		if count != 0 {
			extents = append(extents, start, count)
		}
	}
	return extents
}

func (x blockExtents) chaseOverflow(overflow map[extKey]blockExtents, cnid uint32, isres bool) blockExtents {
	nblocks := uint16(0)
	for i := 1; i < len(x); i += 2 {
		nblocks += x[i]
	}

	for {
		if moreExtents, ok := overflow[extKey{cnid, nblocks, isres}]; ok {
			x = append(x, moreExtents...)

			for i := 1; i < len(moreExtents); i += 2 {
				nblocks += moreExtents[i]
			}
		} else {
			break
		}
	}

	return x
}

func (a blockExtents) toBytes(drAlBlkSiz uint32, drAlBlSt uint16) byteExtents {
	b := make(byteExtents, 0, len(a))
	for i := 0; i < len(a); i += 2 {
		start := int64(a[i])*int64(drAlBlkSiz) + int64(drAlBlSt)*512
		len := int64(a[i+1]) * int64(drAlBlkSiz)
		b = append(b, start, len)
	}
	return b
}

func (a byteExtents) clipExtents(size int64) byteExtents {
	sofar := int64(0)
	for i := 0; i < len(a); i += 2 {
		xsize := a[i+1]

		if xsize > size-sofar {
			xsize = size - sofar
		}

		a[i+1] = xsize

		sofar += xsize

		if xsize == 0 {
			a = a[:i]
			break
		} else if sofar == size {
			a = a[:i+2]
			break
		}
	}
	return a
}

func parseExtentsOverflow(btree []bRecord) map[extKey]blockExtents {
	ret := make(map[extKey]blockExtents)

	for _, record := range btree {
		// xkrKeyLen always 7 in the "current implementation"
		if record[0] != 7 {
			continue
		}

		ret[extKey{
			cnid:  binary.BigEndian.Uint32(record[2:]),
			n:     binary.BigEndian.Uint16(record[6:]),
			isres: record[1] == 0xff,
		}] = parseExtents(record[8:])
	}

	return ret
}

func tryGetSizeCheaply(f io.ReaderAt) (int64, bool) {
	switch as := f.(type) {
	case interface{ Size() int64 }:
		return as.Size(), true
	case fs.File:
		stat, err := as.Stat()
		if err != nil {
			return 0, false
		}
		return stat.Size(), true
	default:
		return 0, false
	}
}

func dirPaths(catalog []bRecord) map[uint32]string {
	tree := make(map[uint32][]dir)
	for _, rec := range catalog {
		parent := binary.BigEndian.Uint32(rec[2:])
		if rec.Val()[0] != 1 { // directories only
			continue
		}
		cnid := binary.BigEndian.Uint32(rec.Val()[6:])
		name := strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":")
		tree[parent] = append(tree[parent], dir{name, cnid})
	}
	m := make(map[uint32]string)
	dirRecurse(m, tree, 1, ".")
	return m
}

func dirRecurse(m map[uint32]string, tree map[uint32][]dir, cnid uint32, name string) {
	for _, ch := range tree[cnid] {
		sub := path.Join(name, ch.name)
		m[ch.cnid] = sub
		dirRecurse(m, tree, ch.cnid, sub)
	}
}

type dir struct {
	name string
	cnid uint32
}

// returned by Sys() on a FileInfo object
// io/fs calls it the "underlying data source"
type ino uint32

func (s ino) Inode() uint64 { return uint64(s) }
