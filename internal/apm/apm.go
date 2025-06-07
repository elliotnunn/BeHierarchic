package apm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// Apple Partition Map
func New(disk io.ReaderAt) (*FS, error) {
	var ddm [514]byte
	n, _ := disk.ReadAt(ddm[:], 0)
	if n < 514 || ddm[0] != 'E' || ddm[1] != 'R' {
		return nil, errors.New("not an Apple Partition Map")
	}

	sbBlkSize := binary.BigEndian.Uint16(ddm[2:])

	// Some CDs had "shadow maps" for buggy ROMs
	// that assumed 512-byte sectors even for 2048-byte CDs.
	// The exact details might be a bit murky here.
	mapEntryStep := int64(sbBlkSize)
	if ddm[512] == 'P' && ddm[513] == 'M' {
		mapEntryStep = 512
	}

	var first [8]byte
	n, _ = disk.ReadAt(first[:], mapEntryStep)
	if n < 8 || first[0] != 'P' || first[1] != 'M' {
		return nil, errors.New("corrupt Apple Partition Map")
	}
	count := int64(binary.BigEndian.Uint32(first[4:8]))

	apm := make([]byte, int(count*mapEntryStep))
	n, _ = disk.ReadAt(apm[:], mapEntryStep)
	if n != len(apm) {
		return nil, errors.New("truncated Apple Partition Map")
	}

	fs := &FS{
		disk:   disk,
		list:   make([]partition, 0, count),
		search: make(map[string]*partition, count),
	}
	for i := range count {
		ent := apm[int64(i)*mapEntryStep:]
		if ent[0] != 'P' || ent[1] != 'M' {
			return nil, errors.New("corrupt Apple Partition Map")
		}
		pmPyPartStart := binary.BigEndian.Uint32(ent[8:])
		pmPartBlkCnt := binary.BigEndian.Uint32(ent[12:])
		pmPartName, _, _ := strings.Cut(string(ent[16:48]), "\x00")
		pmParType, _, _ := strings.Cut(string(ent[48:80]), "\x00")
		pmProcessor, _, _ := strings.Cut(string(ent[120:136]), "\x00")
		var pmPadCode [4]byte
		copy(pmPadCode[:], ent[136:])

		if pmParType == "Apple_Free" {
			continue
		}

		name := pmParType // e.g. Apple_HFS, Apple_Driver43
		if strings.Contains(pmParType, "Driver") {
			if pmProcessor != "" { // e.g. 68000
				name += "," + pmProcessor
			}
			if code := formatPadCode(pmPadCode); code != "" {
				name += "," + code
			}
		} else {
			if pmPartName != "" { // e.g. Macintosh HD
				name += "," + pmPartName
			}
		}

		for n := 0; ; n++ {
			try := name
			if n > 0 {
				try = fmt.Sprintf("%s,%d", try, n)
			}
			if _, dup := fs.search[try]; !dup {
				break
			}
		}

		fs.list = append(fs.list, partition{
			name:   name,
			offset: int64(mapEntryStep) * int64(pmPyPartStart),
			len:    int64(mapEntryStep) * int64(pmPartBlkCnt)})
		fs.search[name] = &fs.list[len(fs.list)-1]
	}
	return fs, nil
}

type partition struct {
	name        string
	offset, len int64
}

type FS struct {
	disk   io.ReaderAt
	list   []partition
	search map[string]*partition
}

func (fsys *FS) Open(name string) (fs.File, error) {
	if name == "." {
		return &openRoot{fsys: fsys}, nil
	} else if part, ok := fsys.search[name]; ok {
		return &openPart{
			SectionReader: io.NewSectionReader(fsys.disk, part.offset, part.len),
			part:          part,
		}, nil
	}
	return nil, fs.ErrNotExist
}

func formatPadCode(c [4]byte) string {
	if string(c[:]) == "\x00\x01\x06\x00" {
		return "SCSI"
	} else if string(c[:]) == "\x00\x00\x00\x00" {
		return ""
	}
	for _, ch := range c {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return fmt.Sprintf("%02x%02x%02x%02x", c[0], c[1], c[2], c[3])
		}
	}
	return string(c[:])
}
