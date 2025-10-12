// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package appledouble

import (
	"encoding/binary"
	"slices"
	"time"
)

var (
	macEpoch         = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	appleDoubleEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	DATA_FORK           = 1
	RESOURCE_FORK       = 2
	REAL_NAME           = 3
	COMMENT             = 4
	ICON_BW             = 5
	ICON_COLOR          = 6
	FILE_INFO_V1        = 7 // Old v1 file info combining FILE_DATES_INFO and MACINTOSH_FILE_INFO.
	FILE_DATES_INFO     = 8
	FINDER_INFO         = 9  // FinderInfo (16) + FinderXInfo (16)
	MACINTOSH_FILE_INFO = 10 // 32 bits, bits 31 = protected and 32 = locked
	PRODOS_FILE_INFO    = 11
	MSDOS_FILE_INFO     = 12
	SHORT_NAME          = 13 // AFP short name.
	AFP_FILE_INFO       = 14
	DIRECTORY_ID        = 15 // AFP directory ID.
)

// Slightly peculiar
func MakePrefix(records map[int][]byte, rforkSize, rForkMinOffset int64) (buf []byte, rForkOffset int64) {
	var keys []int
	var datalen int
	for k, v := range records {
		if k != RESOURCE_FORK {
			keys = append(keys, k)
			datalen += len(v)
		}
	}
	slices.Sort(keys)
	if rforkSize > 0 {
		keys = append(keys, RESOURCE_FORK)
	}

	buf = make([]byte, 26+12*len(keys))
	copy(buf, "\x00\x05\x16\x07\x00\x02\x00\x00") // magic number (modern macOS expects the 07 byte)
	// copy(buf[8:], "Mac OS X        ")             // modern macOS puts this, doesn't seem to be necessary
	binary.BigEndian.PutUint16(buf[24:], uint16(len(keys)))

	for i, key := range keys {
		recOffset := 26 + 12*i
		binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
		binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(len(buf)))

		if key == RESOURCE_FORK {
			rForkOffset = max(int64(len(buf)), rForkMinOffset)
			binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
			binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(rForkOffset))
			binary.BigEndian.PutUint32(buf[recOffset+8:], uint32(rforkSize))
		} else {
			record := records[key]
			binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
			binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(len(buf)))
			binary.BigEndian.PutUint32(buf[recOffset+8:], uint32(len(record)))
			buf = append(buf, record...)
		}
	}
	return
}
