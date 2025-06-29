// Copyright (c) Elliot Nunn

// This library is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 2.1 of the License, or (at your option) any later version.

// This library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.

package sit

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
	"github.com/elliotnunn/BeHierarchic/internal/multireaderat"
)

type forkid int8

const (
	dfork   forkid = 0
	adouble forkid = 1
)

type algid int8

type FS struct {
	root *entry
}

type ForkDebug struct {
	PackOffset, PackSize, UnpackSize uint32
	Algorithm                        int8
	CRC16                            uint16
}

var (
	ErrPassword = errors.New("password protected StuffIt archive")
	ErrAlgo     = errors.New("unknown StuffIt compression algorithm")
)

// Create a new FS from an HFS volume
func New(disk io.ReaderAt) (*FS, error) {
	var buf [80]byte
	n, _ := disk.ReadAt(buf[:], 0)
	if n >= 22 && buf[0] == 'S' && string(buf[10:14]) == "rLau" {
		return newStuffItClassic(disk)
	} else if n == 80 &&
		string(buf[:16]) == "StuffIt (c)1997-" {
		return newStuffIt5(disk)
	} else {
		return nil, errors.New("not a StuffIt file")
	}
}

func newStuffIt5(disk io.ReaderAt) (*FS, error) {
	var buf [6]byte
	_, err := disk.ReadAt(buf[:], 88)
	if err != nil {
		return nil, fmt.Errorf("unreadable StuffIt 5 file: %w", err)
	}

	root := &entry{
		name:  ".",
		isdir: true,
	}
	type j struct {
		next   uint32 // offset into the file
		remain int    // in this directory
		parent *entry
	}
	stack := []j{
		{
			next:   binary.BigEndian.Uint32(buf[0:]),
			remain: int(buf[5]),
			parent: root,
		},
	}

	for len(stack) != 0 {
		job := &stack[len(stack)-1]
		if job.remain == 0 {
			stack = stack[:len(stack)-1]
			continue
		}

		// Progressive disclosure of the header struct
		base := job.next
		r := bufio.NewReaderSize(io.NewSectionReader(disk, int64(base), 0x100000000), 512)
		var hdr1 [48]byte
		if _, err := io.ReadFull(r, hdr1[:]); err != nil {
			goto trunc
		} else if string(hdr1[:4]) != "\xA5\xA5\xA5\xA5" {
			return nil, errors.New("malformed StuffIt 5 header")
		}
		ptr := len(hdr1)
		version := hdr1[4]
		isDir := hdr1[9]&0x40 != 0
		siblingOffset := binary.BigEndian.Uint32(hdr1[22:])
		nameLen := int(binary.BigEndian.Uint16(hdr1[30:]))
		dChildOffset := binary.BigEndian.Uint32(hdr1[34:])
		dCount := int(hdr1[47])
		fDFUnpacked, fDFPacked := binary.BigEndian.Uint32(hdr1[34:]), binary.BigEndian.Uint32(hdr1[38:])
		fDFCRC := binary.BigEndian.Uint16(hdr1[42:])
		fDFFmt := algid(hdr1[46])

		if !isDir { // only files have password data
			discardPassword := int(hdr1[47])
			if _, err := r.Discard(discardPassword); err != nil {
				goto trunc
			}
			ptr += discardPassword
		}

		name := make([]byte, nameLen)
		if _, err := io.ReadFull(r, name); err != nil {
			goto trunc
		}
		ptr += nameLen

		hdr2loc := int(binary.BigEndian.Uint16(hdr1[6:]))
		if _, err := r.Discard(hdr2loc - ptr); err != nil {
			goto trunc
		}
		ptr = hdr2loc

		var hdr2 [32]byte // for directories this can be right at the end of the file
		if _, err := io.ReadFull(r, hdr2[:]); err != nil {
			goto trunc
		}
		ptr += len(hdr2)
		if version <= 1 { // the Mac structure has 4 bytes more than the Windows one
			if _, err := r.Discard(4); err != nil {
				goto trunc
			}
			ptr += 4
		}

		var hdr3 [14]byte             // if no resource fork then this will stay zeroed
		if !isDir && hdr2[1]&1 != 0 { // has resource fork data
			if _, err := io.ReadFull(r, hdr3[:]); err != nil {
				goto trunc
			}
			ptr += len(hdr3)
		}
		fRFUnpacked, fRFPacked := binary.BigEndian.Uint32(hdr3[0:]), binary.BigEndian.Uint32(hdr3[4:])
		fRFCRC := binary.BigEndian.Uint16(hdr3[8:])
		fRFFmt := algid(hdr3[12])

		e := &entry{
			r:        disk,
			isdir:    isDir,
			name:     strings.ReplaceAll(string(name), "/", ":"),
			mactime:  binary.BigEndian.Uint32(hdr1[14:]),
			password: !isDir && hdr1[47] != 0,
		}
		job.remain--
		if job.parent.childMap == nil {
			job.parent.childMap = make(map[string]*entry)
		}
		job.parent.childMap[e.name] = e
		job.parent.childSlice = append(job.parent.childSlice, e)

		if e.isdir {
			hdr2[12] &^= 0x02 // aoceLetter bit can be falsely set -- why?
			hdr2[13] &^= 0xe0 // same with requireSwitchLaunch, isShared, hasNoINITs
			stack = append(stack, j{next: dChildOffset, remain: dCount, parent: e})

			e.forks[adouble] = fork{
				prefix: appledouble.MakePrefix(0, map[int][]byte{
					appledouble.FINDER_INFO:     append(hdr2[4:14:14], make([]byte, 22)...), // location/finderflags
					appledouble.FILE_DATES_INFO: append(hdr1[10:18:18], make([]byte, 8)...), // cr/md/bk/acc
				}),
			}
		} else {
			e.forks[dfork] = fork{
				algo:     fDFFmt,
				packofst: job.next + uint32(ptr) + fRFPacked,
				packsz:   fDFPacked,
				unpacksz: fDFUnpacked,
				crc:      fDFCRC,
			}

			e.forks[adouble] = fork{
				prefix: appledouble.MakePrefix(fRFUnpacked,
					map[int][]byte{
						appledouble.MACINTOSH_FILE_INFO: {hdr1[9] & 0x80, 0, 0, 0},                  // lock bit
						appledouble.FINDER_INFO:         append(hdr2[4:14:14], make([]byte, 22)...), // type/creator/finderflags
						appledouble.FILE_DATES_INFO:     append(hdr1[10:18:18], make([]byte, 8)...), // cr/md/bk/acc
					}),
				algo:     fRFFmt,
				packofst: job.next + uint32(ptr),
				packsz:   fRFPacked,
				unpacksz: fRFUnpacked,
				crc:      fRFCRC,
			}
		}
		job.next = siblingOffset
	}

	return &FS{root: root}, nil
trunc:
	return nil, fmt.Errorf("truncated StuffIt 5 header")
}

func newStuffItClassic(disk io.ReaderAt) (*FS, error) {
	stack := []*entry{{
		name:  ".",
		isdir: true,
	}}

	offset := uint32(22)
	for {
		var hdr [112]byte
		_, err := disk.ReadAt(hdr[:], int64(offset))
		if errors.Is(err, io.EOF) {
			break // normal end of file
		} else if err != nil {
			return nil, fmt.Errorf("unreadable StuffIt file: %w", err)
		}
		offset += 112

		if hdr[0] == 33 { // end of directory
			if len(stack) == 0 {
				return nil, errors.New("malformed StuffIt directory")
			}
			stack = stack[:len(stack)-1]
			continue
		} else if hdr[0] > 33 || hdr[0] < 32 && hdr[0] > 15 {
			return nil, fmt.Errorf("unknown StuffIt record type: %d", hdr[0])
		}

		e := &entry{
			r:        disk,
			isdir:    hdr[0] == 32,
			name:     strings.ReplaceAll(stringFromRoman(hdr[3:66][:min(63, hdr[2])]), "/", ":"),
			mactime:  binary.BigEndian.Uint32(hdr[80:]),
			password: hdr[0]&16 != 0,
		}
		parent := stack[len(stack)-1]
		if parent.childMap == nil {
			parent.childMap = make(map[string]*entry)
		}
		parent.childMap[e.name] = e
		parent.childSlice = append(parent.childSlice, e)

		if e.isdir {
			stack = append(stack, e)

			e.forks[adouble] = fork{
				prefix: appledouble.MakePrefix(0, map[int][]byte{
					appledouble.FINDER_INFO:     append(hdr[66:76:76], make([]byte, 22)...),
					appledouble.FILE_DATES_INFO: append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
				}),
			}
		} else {
			ralgo, dalgo := algid(hdr[0]), algid(hdr[1])
			rpacked, dpacked := binary.BigEndian.Uint32(hdr[92:]), binary.BigEndian.Uint32(hdr[96:])
			runpacked, dunpacked := binary.BigEndian.Uint32(hdr[84:]), binary.BigEndian.Uint32(hdr[88:])
			rcrc, dcrc := binary.BigEndian.Uint16(hdr[100:]), binary.BigEndian.Uint16(hdr[102:])
			roffset, doffset := offset, offset+rpacked

			e.forks[dfork] = fork{
				algo:     dalgo,
				packofst: doffset,
				packsz:   dpacked,
				unpacksz: dunpacked,
				crc:      dcrc,
			}

			e.forks[adouble] = fork{
				prefix: appledouble.MakePrefix(runpacked,
					map[int][]byte{
						appledouble.FINDER_INFO:     append(hdr[66:76:76], make([]byte, 22)...),
						appledouble.FILE_DATES_INFO: append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
					}),
				algo:     ralgo,
				packofst: roffset,
				packsz:   rpacked,
				unpacksz: runpacked,
				crc:      rcrc,
			}

			offset += rpacked + dpacked
		}
	}
	return &FS{root: stack[0]}, nil
}

// To satisfy fs.FS
func (fsys *FS) Open(name string) (fs.File, error) {
	e, forkid, err := fsys.lookupName(name)
	if err != nil {
		return nil, err
	}
	s := stat{e: e, fork: forkid}
	fk := &e.forks[forkid]
	if e.isdir && forkid == dfork {
		return &opendir{s: s}, nil
	} else if e.password {
		return &errorfile{s: s, err: ErrPassword}, nil
	} else if fk.algo == 0 || fk.unpacksz == 0 {
		r1 := bytes.NewReader(fk.prefix)                                       // appledouble header
		r2 := io.NewSectionReader(e.r, int64(fk.packofst), int64(fk.unpacksz)) // fork data
		if len(fk.prefix) != 0 && fk.unpacksz != 0 {
			mr := multireaderat.New(r1, r2)
			return &passthrufile{s: s, r: io.NewSectionReader(mr, 0, mr.Size())}, nil
		} else if len(fk.prefix) != 0 {
			return &passthrufile{s: s, r: r1}, nil
		} else if fk.unpacksz != 0 {
			return &passthrufile{s: s, r: r2}, nil
		} else {
			return &errorfile{s: s, err: io.EOF}, nil
		}
	} else if (algosupport>>fk.algo)&1 != 0 {
		return &openfile{s: s}, nil
	} else {
		return &errorfile{s: s, err: fmt.Errorf("%w number %d", ErrAlgo, fk.algo)}, nil
	}
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	e, forkid, err := fsys.lookupName(name)
	if err != nil {
		return nil, err
	}
	return &stat{e: e, fork: forkid}, nil
}

func (fsys *FS) lookupName(name string) (e *entry, f forkid, err error) {
	components := strings.Split(name, "/")
	if name == "." {
		components = nil
	} else if name == "" {
		return nil, 0, fs.ErrNotExist
	}

	fork := dfork
	if len(components) > 0 {
		var isAD bool
		components[len(components)-1], isAD = strings.CutPrefix(components[len(components)-1], "._")
		if isAD {
			fork = adouble
		}
	}

	e = fsys.root
	for _, c := range components {
		child, ok := e.childMap[c]
		if !ok {
			return nil, 0, fs.ErrNotExist
		}
		e = child
	}
	return e, fork, nil
}

type entry struct {
	r          io.ReaderAt
	name       string
	mactime    uint32
	isdir      bool
	password   bool
	forks      [2]fork
	childSlice []*entry
	childMap   map[string]*entry
}

type fork struct {
	prefix   []byte
	algo     algid
	packsz   uint32
	unpacksz uint32
	packofst uint32
	crc      uint16
}

type stat struct { // both fs.FileInfo and fs.DirEntry
	e    *entry
	fork forkid
}

type openfile struct {
	r io.Reader
	c io.Closer
	s stat
}

type passthrufile struct {
	r interface {
		io.ReadSeeker
		io.ReaderAt
	}
	s stat
}

type errorfile struct {
	err error
	s   stat
}

type opendir struct {
	i int
	s stat
}

func (f *openfile) Stat() (fs.FileInfo, error) {
	return &f.s, nil
}

func (f *openfile) Read(p []byte) (int, error) {
	if f.r == nil {
		fk := &f.s.e.forks[f.s.fork]
		f.r = readerFor(fk.algo, fk.unpacksz, io.NewSectionReader(f.s.e.r, int64(fk.packofst), int64(fk.packsz)))
		if closer, ok := f.r.(io.Closer); ok {
			f.c = closer
		}
		if len(fk.prefix) != 0 {
			f.r = io.MultiReader(bytes.NewReader(fk.prefix), f.r)
		}
	}
	return f.r.Read(p)
}

func (f *openfile) Close() error {
	if f.r != nil {
		if closer, ok := f.r.(io.Closer); ok {
			return closer.Close()
		}
	}
	return nil
}

func (f *passthrufile) Stat() (fs.FileInfo, error) {
	return &f.s, nil
}

func (f *passthrufile) Read(p []byte) (int, error) {
	return f.r.Read(p)
}

func (f *passthrufile) ReadAt(p []byte, off int64) (int, error) {
	return f.r.ReadAt(p, off)
}

func (f *passthrufile) Seek(offset int64, whence int) (int64, error) {
	return f.r.Seek(offset, whence)
}

func (f *passthrufile) Close() error {
	return nil
}

func (f *errorfile) Stat() (fs.FileInfo, error) {
	return &f.s, nil
}

func (f *errorfile) Read(p []byte) (int, error) {
	return 0, f.err
}

func (f *errorfile) ReadAt(p []byte, off int64) (int, error) {
	return 0, f.err
}

func (f *errorfile) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (f *errorfile) Close() error {
	return nil
}

func (f *opendir) Stat() (fs.FileInfo, error) {
	return &f.s, nil
}

func (f *opendir) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (f *opendir) Close() error {
	return nil
}

// To satisfy fs.ReadDirFile, has slightly tricky partial-listing semantics
func (f *opendir) ReadDir(count int) ([]fs.DirEntry, error) {
	n := len(f.s.e.childSlice)*2 - f.i
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}
	list := make([]fs.DirEntry, n)
	for i := range list {
		list[i] = &stat{
			e:    f.s.e.childSlice[(f.i+i)/2],
			fork: forkid((f.i + i) % 2),
		}
	}
	f.i += n
	return list, nil
}

// IsDir() bool
// Name() string
// Size() int64
// Mode() FileMode
// Type() FileMode
// ModTime() time.Time
// Sys() any
// Info() (FileInfo, error)

func (s stat) IsDir() bool {
	return s.e.isdir && s.fork == dfork
}

func (s stat) Name() string {
	if s.fork == dfork {
		return s.e.name
	} else {
		return "._" + s.e.name
	}
}

func (s stat) Size() int64 {
	return int64(len(s.e.forks[s.fork].prefix)) + int64(s.e.forks[s.fork].unpacksz)
}

func (s stat) Mode() fs.FileMode {
	if s.IsDir() {
		return fs.ModeDir
	} else {
		return 0
	}
}

func (s stat) Type() fs.FileMode {
	if s.IsDir() {
		return fs.ModeDir
	} else {
		return 0
	}
}

func (s stat) ModTime() time.Time {
	return time.Unix(int64(s.e.mactime)-2082844800, 0).UTC()
}

func (s stat) Sys() any {
	fk := &s.e.forks[s.fork]
	return &ForkDebug{
		PackOffset: fk.packofst,
		PackSize:   fk.packsz,
		UnpackSize: fk.unpacksz,
		Algorithm:  int8(fk.algo),
		CRC16:      fk.crc,
	}
}

func (s stat) Info() (fs.FileInfo, error) {
	return s, nil
}

const algosupport = 0b1010_0000_0000_1101

func readerFor(algo algid, unpacksz uint32, r io.Reader) io.Reader { // might also be ReadCloser
	// corpus includes algo 0, 2, 3, 5, 13, 15
	switch algo {
	case 0: // no compression
		return r
	// case 1: // RLE compression
	case 2: // LZC compression
		return lzc(r, unpacksz)
	case 3: // Huffman compression
		return huffman(r, unpacksz)
	// case 5: // LZ with adaptive Huffman
	// case 6: // Fixed Huffman table
	// case 8: // Miller-Wegman encoding
	case 13: // anonymous
		return sit13(r, unpacksz)
	// case 14: // anonymous
	case 15: // Arsenic
		return arsenic(r, unpacksz)
	default:
		panic("should not attempt readerFor on unsupported algo... update algosupport")
	}
}
