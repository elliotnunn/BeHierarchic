// Copyright (c) 2021, 2024 Elliot Nunn
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
	"strings"
	"time"
)

// For accessing Macintosh resource forks:
// wfahen opening a file with this appended to the path, it will have
// the same Stat metadata but a different size and different data
const ResForkSuffix = "//resourcefork"

type FS struct {
	root *entry
}

// Populated by New, and accessed through the functions & interfaces of io/fs:
// satisfies both the "directory listing" and "stat" interfaces, because
// in this implementation it does not cost extra to retrieve the "stat" info.
type entry struct {
	name       string
	size       int64
	modtime    time.Time
	isdir      bool
	disk       io.ReaderAt
	extents    byteExtents
	childSlice []*entry
	childMap   map[string]*entry
	resfork    *entry
	sys        Sys
}

// HFS specific metadata exposed via fs.FileInfo.Sys().(*Sys)
type Sys struct {
	ID           uint32   // catalog node ID (CNID, roughly an inode number)
	Flags        uint16   // rather obscure except for the lock bit (which is?)
	FinderInfo   [32]byte // concatenation of FInfo+FXInfo or DInfo+DXInfo
	CreationTime time.Time
	BackupTime   time.Time
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
			disk:  disk,
			name:  ".",
			isdir: true,
			sys: Sys{
				ID: 1,
			},
		},
	}
	childrenof := make(map[uint32][]*entry)

	for _, rec := range catalog {
		cut := (int(rec[0]) + 2) &^ 1
		val := rec[cut:]

		switch val[0] { // so-called "thread" records ignored
		case 1: // dir
			parent := binary.BigEndian.Uint32(rec[2:])
			name := strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":")
			cnid := binary.BigEndian.Uint32(val[6:])

			e := entry{
				disk:    disk,
				name:    name,
				isdir:   true,
				modtime: macTime(val[0xe:]),
				sys: Sys{
					ID:           cnid,
					Flags:        binary.BigEndian.Uint16(val[2:]),
					CreationTime: macTime(val[0xa:]),
					BackupTime:   macTime(val[0x12:]),
				},
			}
			copy(e.sys.FinderInfo[:], val[0x16:0x36]) // DInfo & DXInfo

			entryof[cnid] = &e
			childrenof[parent] = append(childrenof[parent], &e)

		case 2: // file
			parent := binary.BigEndian.Uint32(rec[2:])
			name := strings.ReplaceAll(stringFromRoman(rec[7:][:rec[6]]), "/", ":")
			cnid := binary.BigEndian.Uint32(val[0x14:])

			dfork := entry{
				disk:    disk,
				name:    name,
				isdir:   false,
				modtime: macTime(val[0x30:]),
				sys: Sys{
					ID:           cnid,
					Flags:        binary.BigEndian.Uint16(val[2:]),
					CreationTime: macTime(val[0x2c:]),
					BackupTime:   macTime(val[0x34:]),
				},
			}
			copy(dfork.sys.FinderInfo[0:0x10], val[0x4:0x14])     // FInfo
			copy(dfork.sys.FinderInfo[0x10:0x20], val[0x38:0x48]) // FXInfo

			rfork := dfork         // resource fork is near identical
			dfork.resfork = &rfork // but data fork has a ptr to it

			for _, isResFork := range [...]bool{true, false} {
				sizeField, extentField, entry := 0x1a, 0x4a, &dfork
				if isResFork {
					sizeField, extentField, entry = 0x24, 0x56, &rfork
				}

				entry.size = int64(binary.BigEndian.Uint32(val[sizeField:]))
				entry.extents =
					parseExtents(val[extentField:]).
						chaseOverflow(overflow, cnid, isResFork).
						toBytes(drAlBlkSiz, drAlBlSt).
						clipExtents(entry.size)
			}

			entryof[cnid] = &dfork
			childrenof[parent] = append(childrenof[parent], &dfork)
		}
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

func (x byteExtents) makeReader(fs io.ReaderAt) *multiReaderAt {
	return &multiReaderAt{backing: fs, extents: x}
}

func mustReadAll(r io.Reader) []byte {
	slice, err := io.ReadAll(r)
	if err != nil {
		panic("unable to read a special file")
	}
	return slice
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

// To satisfy fs.FileInfo and fs.DirEntry
func (f *entry) Name() string {
	return f.name
}

// To satisfy fs.FileInfo and fs.DirEntry
func (f *entry) IsDir() bool {
	return f.isdir
}

// To satisfy fs.FileInfo
func (f *entry) Size() int64 {
	return f.size
}

// To satisfy fs.FileInfo
func (f *entry) ModTime() time.Time {
	return f.modtime
}

// To satisfy fs.FileInfo
func (f *entry) Sys() interface{} {
	return &f.sys
}

// To satisfy fs.FileInfo
func (f *entry) Mode() fs.FileMode {
	if f.isdir {
		return fs.ModeDir
	} else {
		return 0
	}
}

// To satisfy fs.DirEntry
func (f *entry) Info() (fs.FileInfo, error) {
	return f, nil
}

// To satisfy fs.DirEntry
// Supposedly a cheaper subset of "Mode" for listing without statting
func (f *entry) Type() fs.FileMode {
	return f.Mode() & fs.ModeType
}

type openfile struct {
	entry          *entry
	*multiReaderAt     // To satisfy fs.File (and io.ReaderAt/ReadSeeker as a bonus)
	listOffset     int // for listing directory
}

// To satisfy fs.FS
func (fsys FS) Open(name string) (fs.File, error) {
	name, resfork := strings.CutSuffix(name, ResForkSuffix)

	components := strings.Split(name, "/")
	if name == "." {
		components = nil
	} else if name == "" {
		return nil, fs.ErrNotExist
	}

	e := fsys.root
	for _, c := range components {
		child, ok := e.childMap[c]
		if !ok {
			return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, name)
		}
		e = child
	}

	if resfork {
		if e.isdir {
			return nil, fs.ErrNotExist
		} else {
			e = e.resfork
		}
	}

	if e.isdir {
		return &openfile{entry: e}, nil
	} else {
		return &openfile{entry: e, multiReaderAt: e.extents.makeReader(e.disk)}, nil
	}
}

// To satisfy fs.File
func (f *openfile) Stat() (fs.FileInfo, error) {
	return f.entry, nil
}

// To satisfy fs.File
func (f *openfile) Close() error {
	return nil
}

// To satisfy fs.ReadDirFile, has slightly tricky partial-listing semantics
func (f *openfile) ReadDir(count int) ([]fs.DirEntry, error) {
	n := len(f.entry.childSlice) - f.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	for i := range list {
		list[i] = f.entry.childSlice[f.listOffset+i]
	}
	f.listOffset += n
	return list, nil
}
