package sit

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

// Create a new FS from an HFS volume
func New(disk io.ReaderAt) (*FS, error) {
	var buf [14]byte
	_, err := disk.ReadAt(buf[:], 0)
	if err != nil || buf[0] != 'S' || string(buf[10:14]) != "rLau" {
		return nil, errors.New("not a StuffIt file")
	}

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
						appledouble.MACINTOSH_FILE_INFO: append(hdr[74:76:76], make([]byte, 2)...),
						appledouble.FINDER_INFO:         make([]byte, 32),
						appledouble.FILE_DATES_INFO:     append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
					},
					nil,
				),
			}
		} else {
			rsize, dsize := int64(binary.BigEndian.Uint32(hdr[92:])), int64(binary.BigEndian.Uint32(hdr[96:]))
			rreader := io.NewSectionReader(disk, offset, rsize)
			dreader := io.NewSectionReader(disk, offset+rsize, dsize)
			offset += rsize + dsize
			e.fork = [2]multireaderat.SizeReaderAt{
				readerFor(hdr[1], dreader, int64(binary.BigEndian.Uint32(hdr[88:]))),
				appledouble.Make(
					map[int][]byte{
						appledouble.MACINTOSH_FILE_INFO: append(hdr[74:76:76], make([]byte, 2)...),
						appledouble.FINDER_INFO:         append(hdr[66:74:74], make([]byte, 24)...),
						appledouble.FILE_DATES_INFO:     append(hdr[76:84:84], make([]byte, 8)...), // cr/md/bk/acc
					},
					map[int]multireaderat.SizeReaderAt{
						appledouble.RESOURCE_FORK: readerFor(hdr[0], rreader, int64(binary.BigEndian.Uint32(hdr[84:]))),
					},
				),
			}
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
	return nil
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

func readerFor(method byte, compr multireaderat.SizeReaderAt, size int64) multireaderat.SizeReaderAt {
	return bytes.NewReader(make([]byte, size))
}
