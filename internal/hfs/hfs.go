// Copyright (c) 2021, 2024 Elliot Nunn
// Licensed under the MIT license

// Implement fs.FS for Apple's very old Hierarchical File System
// (plain HFS from 1985, not to be confused with HFS+)
package hfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/elliotnunn/resourceform/internal/appledouble"
	"github.com/elliotnunn/resourceform/internal/multireaderat"
)

type FS struct {
	root *entry
}

type entry struct {
	name       string
	modtime    time.Time
	isdir      bool
	fork       [2]multireaderat.SizeReaderAt // {datafork, appledouble}
	childSlice []*entry
	childMap   map[string]*entry
	cnid       uint32 // catalog node ID (CNID, roughly an inode number)
}

// Create a new FS from an HFS volume
func New(disk io.ReaderAt) (retfs *FS, reterr error) {
	defer func() {
		if r := recover(); r != nil {
			retfs, reterr = nil, fmt.Errorf("%v", r)
		}
	}()

	var mdb [512]byte
	_, err := disk.ReadAt(mdb[:], 0x400)
	if err != nil {
		return nil, fmt.Errorf("Master Directory Block unreadable: %w", err)
	}

	if mdb[0] != 'B' || mdb[1] != 'D' {
		return nil, errors.New("HFS magic number absent")
	}

	drNmAlBlks := binary.BigEndian.Uint16(mdb[0x12:])
	drAlBlkSiz := binary.BigEndian.Uint32(mdb[0x14:])
	drAlBlSt := binary.BigEndian.Uint16(mdb[0x1c:])

	// Ensure that the last block is readable before getting a sad surprise
	var testsec [512]byte
	minlen := int64(drAlBlSt)*512 + int64(drAlBlkSiz)*int64(drNmAlBlks)
	_, err = disk.ReadAt(testsec[:], minlen-int64(len(testsec)))
	if err != nil {
		return nil, fmt.Errorf("volume should be %d bytes but is truncated", minlen)
	}

	// Open question: can the extents overflow file depend on extents stored in itself?
	overflow :=
		parseExtentsOverflow(
			parseBTree(
				mustReadAll(
					parseExtents(mdb[0x86:]).
						toBytes(drAlBlkSiz, drAlBlSt).
						makeReader(disk))))

	catalog :=
		parseBTree(
			mustReadAll(
				parseExtents(
					mdb[0x96:]).
					chaseOverflow(overflow, 4, false).
					toBytes(drAlBlkSiz, drAlBlSt).
					makeReader(disk)))

	// These maps are needed because children can come before parents in the catalog
	entryof := map[uint32]*entry{
		1: {
			name:  ".",
			isdir: true,
			cnid:  1,
		},
	}
	childrenof := make(map[uint32][]*entry)

	for _, rec := range catalog {
		cut := (int(rec[0]) + 2) &^ 1
		val := rec[cut:]

		var e entry
		parent := binary.BigEndian.Uint32(rec[2:])
		switch val[0] {
		default: // so-called "thread" records ignored
			continue
		case 1: // dir
			e = entry{
				name:    strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":"),
				cnid:    binary.BigEndian.Uint32(val[6:]),
				isdir:   true,
				modtime: macTime(val[0xe:]),
				fork: [2]multireaderat.SizeReaderAt{
					nil, // no data fork
					bytes.NewReader(appledouble.MakePrefix(0, map[int][]byte{
						appledouble.MACINTOSH_FILE_INFO: append(val[2:4:4], make([]byte, 2)...),
						appledouble.FINDER_INFO:         val[0x16:0x36],
						appledouble.FILE_DATES_INFO:     append(val[0xa:0x16:0x16], make([]byte, 4)...), // cr/md/bk/acc
					})),
				},
			}
		case 2: // file
			cnid := binary.BigEndian.Uint32(val[0x14:])
			rfSize := binary.BigEndian.Uint32(val[0x24:])
			e = entry{
				name:    strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":"),
				cnid:    cnid,
				isdir:   false,
				modtime: macTime(val[0x30:]),
				fork: [2]multireaderat.SizeReaderAt{
					parseExtents(val[0x4a:]).
						chaseOverflow(overflow, cnid, false).
						toBytes(drAlBlkSiz, drAlBlSt).
						clipExtents(int64(binary.BigEndian.Uint32(val[0x1a:]))).
						makeReader(disk), // the data fork
					multireaderat.New(
						bytes.NewReader(appledouble.MakePrefix(rfSize,
							map[int][]byte{
								appledouble.MACINTOSH_FILE_INFO: append(val[2:4:4], make([]byte, 2)...),
								appledouble.FINDER_INFO:         append(val[0x4:0x14:0x14], val[0x38:0x48]...),
								appledouble.FILE_DATES_INFO:     append(val[0x2c:0x38:0x38], make([]byte, 4)...), // cr/md/bk/acc
							})), // the appledouble header
						parseExtents(val[0x56:]).
							chaseOverflow(overflow, cnid, true).
							toBytes(drAlBlkSiz, drAlBlSt).
							clipExtents(int64(binary.BigEndian.Uint32(val[0x24:]))).
							makeReader(disk), // followed by the resource fork
					),
				},
			}
		}
		if strings.HasPrefix(e.name, "._") {
			continue // very unusual for an HFS volume to contain an AppleDouble file
		}
		entryof[e.cnid] = &e
		childrenof[parent] = append(childrenof[parent], &e)
	}

	// Sew up those two maps
	for cnid, e := range entryof {
		e.childSlice = childrenof[cnid]
		e.childMap = make(map[string]*entry)
		for _, child := range e.childSlice {
			e.childMap[child.name] = child
		}
	}

	// Keep the "parent of root" element, which has exactly one child, the disk
	return &FS{entryof[1]}, nil
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

func (x byteExtents) makeReader(fs io.ReaderAt) multireaderat.SizeReaderAt {
	subreaders := make([]multireaderat.SizeReaderAt, 0, len(x)/2)
	for i := 0; i < len(x); i += 2 {
		xstart, xlen := x[i], x[i+1]
		subreaders = append(subreaders, io.NewSectionReader(fs, xstart, xlen))
	}
	return multireaderat.New(subreaders...)
}

func mustReadAll(r multireaderat.SizeReaderAt) []byte {
	b := make([]byte, r.Size())
	_, err := r.ReadAt(b, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		panic("unable to read a special file")
	}
	return b
}

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
	if sofar != size {
		panic("not enough extents to satisfy logical length")
	}
	return a
}

func parseBTree(tree []byte) (records [][]byte) {
	// Special first node has special header record
	headerRec := parseBNode(tree)[0]

	// Ends of a linked list of leaf nodes
	bthFNode := int(binary.BigEndian.Uint32(headerRec[10:]))
	bthLNode := int(binary.BigEndian.Uint32(headerRec[14:]))

	i := bthFNode
	for {
		offset := 512 * i
		records = append(records, parseBNode(tree[offset:][:512])...)

		if i == bthLNode {
			break
		}
		i = int(binary.BigEndian.Uint32(tree[offset:]))
	}

	return records
}

func parseBNode(node []byte) [][]byte {
	cnt := int(binary.BigEndian.Uint16(node[10:]))

	boundaries := make([]int, 0, cnt+1)
	for i := 0; i < cnt+1; i++ {
		boundaries = append(boundaries, int(binary.BigEndian.Uint16(node[512-2-2*i:])))
	}

	records := make([][]byte, 0, cnt)
	for i := 0; i < cnt; i++ {
		start := boundaries[i]
		stop := boundaries[i+1]
		records = append(records, node[start:stop])
	}

	return records
}

func parseExtentsOverflow(btree [][]byte) map[extKey]blockExtents {
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

// For reproducibility, pretends that all times on the volume mean UTC,
// even though they were unfortunately set to local time and the TZ discarded
func macTime(field []byte) time.Time {
	stamp := binary.BigEndian.Uint32(field)
	return time.Unix(int64(stamp)-2082844800, 0).UTC()
}

// To satisfy fs.FS
func (fsys FS) Open(name string) (fs.File, error) {
	components := strings.Split(name, "/")
	if name == "." {
		components = nil
	} else if name == "" {
		return nil, fs.ErrNotExist
	}

	sidecar := false
	if len(components) > 0 {
		components[len(components)-1], sidecar = strings.CutPrefix(components[len(components)-1], "._")
	}

	e := fsys.root
	for _, c := range components {
		child, ok := e.childMap[c]
		if !ok {
			return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, name)
		}
		e = child
	}
	return open(e, sidecar), nil
}

func open(e *entry, sidecar bool) *openfile {
	f := openfile{e: e, sidecar: sidecar}
	if sidecar {
		f.rsrs = io.NewSectionReader(e.fork[1], 0, e.fork[1].Size())
	} else if !e.isdir {
		f.rsrs = io.NewSectionReader(e.fork[0], 0, e.fork[0].Size())
	} else {
		f.rsrs = bytes.NewReader(nil)
	}
	return &f
}

type openfile struct {
	rsrs
	e          *entry // for Name/Mode/Type/ModTime/Sys
	sidecar    bool   // for IsDir
	listOffset int    // for ReadDir
	// also need Info/Stat/Close
}

type rsrs interface {
	Read([]byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
	ReadAt([]byte, int64) (int, error)
	Size() int64
}

func (f *openfile) Name() string { // implements fs.FileInfo and fs.DirEntry
	if f.sidecar {
		return "._" + f.e.name
	} else {
		return f.e.name
	}
}

func (f *openfile) Mode() fs.FileMode { // implements fs.FileInfo
	if f.IsDir() {
		return fs.ModeDir
	} else {
		return 0
	}
}

func (f *openfile) Type() fs.FileMode { // implements fs.DirEntry
	if f.IsDir() {
		return fs.ModeDir
	} else {
		return 0
	}
}

func (f *openfile) ModTime() time.Time { // implements fs.FileInfo
	return f.e.modtime
}

func (f *openfile) Sys() any { // implements fs.FileInfo
	return f.e.cnid
}

func (f *openfile) IsDir() bool { // implements fs.FileInfo and fs.DirEntry
	return f.e.isdir && !f.sidecar
}

// To satisfy fs.ReadDirFile, has slightly tricky partial-listing semantics
func (f *openfile) ReadDir(count int) ([]fs.DirEntry, error) {
	n := len(f.e.childSlice)*2 - f.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	for i := range list {
		actualFile := f.e.childSlice[(f.listOffset+i)/2]
		isSidecar := (f.listOffset+i)%2 == 1
		list[i] = open(actualFile, isSidecar)
	}
	f.listOffset += n
	return list, nil
}

func (f *openfile) Info() (fs.FileInfo, error) { // implements fs.DirEntry
	return f, nil
}

func (f *openfile) Stat() (fs.FileInfo, error) { // implements fs.File
	return f, nil
}

func (f *openfile) Close() error { // implements fs.File
	return nil
}
