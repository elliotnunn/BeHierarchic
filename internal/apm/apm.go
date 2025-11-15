// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package apm

import (
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
)

// Apple Partition Map
func New(disk io.ReaderAt) (fs.FS, error) {
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

	fsys := fskeleton.New()
	defer fsys.NoMore()

	var entries [][]byte
	for i := range count {
		ent := apm[int64(i)*mapEntryStep:][:512]
		if ent[0] != 'P' || ent[1] != 'M' {
			return nil, errors.New("corrupt Apple Partition Map")
		}
		entries = append(entries, ent[:512])
	}

	// Put them disk order
	slices.SortStableFunc(entries, func(a, b []byte) int {
		return cmp.Compare(binary.BigEndian.Uint32(a[8:]), binary.BigEndian.Uint32(b[8:]))
	})

	ofeach := make(map[string]int)
	for _, ent := range entries {
		pmPyPartStart := binary.BigEndian.Uint32(ent[8:])
		pmPartBlkCnt := binary.BigEndian.Uint32(ent[12:])
		// pmPartName, _, _ := strings.Cut(string(ent[16:48]), "\x00") // does not reflect later name changes
		pmParType, _, _ := strings.Cut(string(ent[48:80]), "\x00")
		// pmProcessor, _, _ := strings.Cut(string(ent[120:136]), "\x00")
		var pmPadCode [4]byte
		copy(pmPadCode[:], ent[136:])

		if pmParType == "Apple_Free" {
			continue
		}

		name := pmParType // e.g. Apple_HFS, Apple_Driver43
		name = strings.TrimPrefix(name, "Apple_")
		name = strings.ToLower(name)
		ofeach[name]++
		name += "-" + strconv.Itoa(ofeach[name])

		pstart, plen := int64(mapEntryStep)*int64(pmPyPartStart), int64(mapEntryStep)*int64(pmPartBlkCnt)

		fsys.CreateReaderAtFile(name, pstart,
			io.NewSectionReader(disk, pstart, plen), plen, 0, time.Time{}, nil)
	}
	return fsys, nil
}
