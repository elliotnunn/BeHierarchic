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

	"github.com/elliotnunn/resourceform/internal/appledouble"
	"github.com/elliotnunn/resourceform/internal/decompressioncache"
	"github.com/elliotnunn/resourceform/internal/multireaderat"
)

type FS struct {
	root *entry
}

// fs.FileInfo.Sys() method returns [2]ForkDebug for data/resource
type ForkDebug struct {
	PackOffset, PackSize, UnpackSize int64
	Algorithm                        int
}

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
		next   int64 // offset into the file
		remain int   // in this directory
		parent *entry
	}
	stack := []j{
		{
			next:   int64(binary.BigEndian.Uint32(buf[0:])),
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
		r := bufio.NewReaderSize(io.NewSectionReader(disk, base, 0x100000000), 512)
		var hdr1 [48]byte
		if _, err := io.ReadFull(r, hdr1[:]); err != nil {
			goto trunc
		} else if string(hdr1[:4]) != "\xA5\xA5\xA5\xA5" {
			return nil, errors.New("malformed StuffIt 5 header")
		}
		ptr := len(hdr1)
		version := hdr1[4]
		isDir := hdr1[9]&0x40 != 0
		siblingOffset := int64(binary.BigEndian.Uint32(hdr1[22:]))
		nameLen := int(binary.BigEndian.Uint16(hdr1[30:]))
		dChildOffset := int64(binary.BigEndian.Uint32(hdr1[34:]))
		dCount := int(hdr1[47])
		fDFUnpacked, fDFPacked := int64(binary.BigEndian.Uint32(hdr1[34:])), int64(binary.BigEndian.Uint32(hdr1[38:]))
		fDFFmt := hdr1[46]

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
		fRFUnpacked, fRFPacked := int64(binary.BigEndian.Uint32(hdr3[0:])), int64(binary.BigEndian.Uint32(hdr3[4:]))
		fRFFmt := hdr3[12]

		e := &entry{
			isdir:   isDir,
			name:    strings.ReplaceAll(string(name), "/", ":"),
			modtime: time.Unix(int64(binary.BigEndian.Uint32(hdr1[14:]))-2082844800, 0).UTC(),
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
			e.fork = [2]multireaderat.SizeReaderAt{
				nil, // no data fork for directories
				appledouble.Make(
					map[int][]byte{
						appledouble.FINDER_INFO:     append(hdr2[4:14:14], make([]byte, 22)...), // location/finderflags
						appledouble.FILE_DATES_INFO: append(hdr1[10:18:18], make([]byte, 8)...), // cr/md/bk/acc
					},
					nil,
				),
			}
		} else {
			e.fork = [2]multireaderat.SizeReaderAt{
				readerFor(fDFFmt, io.NewSectionReader(disk, job.next+int64(ptr)+fRFPacked, fDFPacked), fDFUnpacked, e.name),
				appledouble.Make(
					map[int][]byte{
						appledouble.MACINTOSH_FILE_INFO: {hdr1[9] & 0x80, 0, 0, 0},                  // lock bit
						appledouble.FINDER_INFO:         append(hdr2[4:14:14], make([]byte, 22)...), // type/creator/finderflags
						appledouble.FILE_DATES_INFO:     append(hdr1[10:18:18], make([]byte, 8)...), // cr/md/bk/acc
					},
					map[int]multireaderat.SizeReaderAt{
						appledouble.RESOURCE_FORK: readerFor(fRFFmt, io.NewSectionReader(disk, job.next+int64(ptr), fRFPacked), fRFUnpacked, e.name+".rsrc"),
					},
				),
			}
			e.dbg = [2]ForkDebug{
				{PackOffset: job.next + int64(ptr) + fRFPacked, PackSize: fDFPacked, UnpackSize: fDFUnpacked, Algorithm: int(fDFFmt)},
				{PackOffset: job.next + int64(ptr), PackSize: fRFPacked, UnpackSize: fRFUnpacked, Algorithm: int(fRFFmt)},
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

	offset := int64(22)
	for {
		var hdr [112]byte
		_, err := disk.ReadAt(hdr[:], offset)
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
			isdir:   hdr[0] == 32,
			name:    strings.ReplaceAll(stringFromRoman(hdr[3:66][:min(63, hdr[2])]), "/", ":"),
			modtime: time.Unix(int64(binary.BigEndian.Uint32(hdr[80:]))-2082844800, 0).UTC(),
		}
		parent := stack[len(stack)-1]
		if parent.childMap == nil {
			parent.childMap = make(map[string]*entry)
		}
		parent.childMap[e.name] = e
		parent.childSlice = append(parent.childSlice, e)

		if e.isdir {
			stack = append(stack, e)
			e.fork = [2]multireaderat.SizeReaderAt{
				nil, // no data fork for directories
				appledouble.Make(
					map[int][]byte{
						appledouble.FINDER_INFO:     append(hdr[66:76:76], make([]byte, 22)...),
						appledouble.FILE_DATES_INFO: append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
					},
					nil,
				),
			}
		} else {
			ralgo, dalgo := hdr[0], hdr[1]
			rpacked, dpacked := int64(binary.BigEndian.Uint32(hdr[92:])), int64(binary.BigEndian.Uint32(hdr[96:]))
			runpacked, dunpacked := int64(binary.BigEndian.Uint32(hdr[84:])), int64(binary.BigEndian.Uint32(hdr[88:]))
			roffset, doffset := offset, offset+rpacked
			e.fork = [2]multireaderat.SizeReaderAt{
				readerFor(dalgo, io.NewSectionReader(disk, doffset, dpacked), dunpacked, e.name),
				appledouble.Make(
					map[int][]byte{
						appledouble.FINDER_INFO:     append(hdr[66:76:76], make([]byte, 22)...),
						appledouble.FILE_DATES_INFO: append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
					},
					map[int]multireaderat.SizeReaderAt{
						appledouble.RESOURCE_FORK: readerFor(ralgo, io.NewSectionReader(disk, roffset, rpacked), runpacked, e.name),
					},
				),
			}
			e.dbg = [2]ForkDebug{
				{PackOffset: doffset, PackSize: dpacked, UnpackSize: dunpacked, Algorithm: int(dalgo)},
				{PackOffset: roffset, PackSize: rpacked, UnpackSize: runpacked, Algorithm: int(ralgo)},
			}
			offset += rpacked + dpacked
		}
	}
	return &FS{root: stack[0]}, nil
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

type entry struct {
	name       string
	modtime    time.Time
	isdir      bool
	fork       [2]multireaderat.SizeReaderAt // {datafork, appledouble}
	childSlice []*entry
	childMap   map[string]*entry
	dbg        [2]ForkDebug
}

type openfile struct {
	rsrs
	e          *entry // for Name/Mode/Type/ModTime/Sys
	sidecar    bool   // for IsDir
	listOffset int    // for ReadDir
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
	return f.e.dbg
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

func readerFor(method byte, compr multireaderat.SizeReaderAt, size int64, name string) multireaderat.SizeReaderAt {
	// corpus includes algo 0, 2, 3, 5, 13, 15
	switch method {
	default:
		return bytes.NewReader(make([]byte, size)) // dodgy temporary
	case 0: // no compression
		return compr
	// case 3: // Huffman compression
	// 	return decompressioncache.New(InitHuffman(compr, size), size, name)
	// case 1: // RLE compression
	// case 2: // LZC compression
	// case 5: // LZ with adaptive Huffman
	// case 6: // Fixed Huffman table
	// case 8: // Miller-Wegman encoding
	case 13: // anonymous
		return decompressioncache.New(InitSIT13(compr, size), size, name)
	// case 14: // anonymous
	case 15: // Arsenic
		return decompressioncache.New(InitArsenic(compr, size), size, name)
	}
}
