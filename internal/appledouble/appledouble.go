// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package appledouble

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
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
func MakePrefix(rforkSize uint32, shortRecs map[int][]byte) []byte {
	var keys []int
	for k := range shortRecs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if rforkSize > 0 {
		keys = append(keys, RESOURCE_FORK)
	}

	buf := make([]byte, 26+12*len(keys), 8192)
	copy(buf, "\x00\x05\x16\x07\x00\x02\x00\x00") // magic number (modern macOS expects the 07 byte)
	// copy(buf[8:], "Mac OS X        ")             // modern macOS puts this, doesn't seem to be necessary
	binary.BigEndian.PutUint16(buf[24:], uint16(len(keys)))

	for i, key := range keys {
		recOffset := 26 + 12*i
		binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
		binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(len(buf)))

		if key == RESOURCE_FORK {
			buf = buf[:cap(buf)]
			binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
			binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(cap(buf)))
			binary.BigEndian.PutUint32(buf[recOffset+8:], rforkSize)
		} else {
			binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
			binary.BigEndian.PutUint32(buf[recOffset+4:], uint32(len(buf)))
			binary.BigEndian.PutUint32(buf[recOffset+8:], uint32(len(shortRecs[key])))
			buf = append(buf, shortRecs[key]...)
		}
	}

	return buf
}

var admap = map[int]string{
	1:  "DATA_FORK",
	2:  "RESOURCE_FORK",
	3:  "REAL_NAME",
	4:  "COMMENT",
	5:  "ICON_BW",
	6:  "ICON_COLOR",
	7:  "FILE_INFO_V1",
	8:  "FILE_DATES_INFO",
	9:  "FINDER_INFO",
	10: "MACINTOSH_FILE_INFO",
	11: "PRODOS_FILE_INFO",
	12: "MSDOS_FILE_INFO",
	13: "SHORT_NAME",
	14: "AFP_FILE_INFO",
	15: "DIRECTORY_ID",
}

func Dump(r io.Reader) (string, error) {
	buf := make([]byte, 4096)
	n, err := r.Read(buf)
	if n < 26 || n < 26+12*int(binary.BigEndian.Uint16(buf[24:])) {
		return "", fmt.Errorf("truncated appledouble (%d bytes): %w", n, err)
	}
	buf = buf[:n]

	buf[3] = 0x00 // is 7 in other implementations??
	if string(buf[:8]) != "\x00\x05\x16\x00\x00\x02\x00\x00" {
		return "", errors.New("not an appledouble" + hex.EncodeToString(buf[:8]))
	}

	count := binary.BigEndian.Uint16(buf[24:])
	var bild strings.Builder
	for i := range count {
		kind := binary.BigEndian.Uint32(buf[26+12*i:])
		offset := binary.BigEndian.Uint32(buf[26+12*i+4:])
		size := binary.BigEndian.Uint32(buf[26+12*i+8:])
		name := admap[int(kind)]
		if name == "" {
			name = fmt.Sprintf("UNKNOWN_%X", kind)
		}

		val := fmt.Sprintf("%#x:%#x", offset, offset+size)
		if offset+size <= uint32(len(buf)) { // not a big fork
			data := buf[offset : offset+size]
			switch kind {
			case 8: // FILE_DATES_INFO
				val = formatDates(data)
			case 9: // FINDER_INFO // differs between files and directories
				val = formatFinderInfo(data)
			case 10: // MACINTOSH_FILE_INFO
				val = formatOtherInfo(data)
			}
		}
		if bild.Len() > 0 {
			bild.WriteByte('\n')
		}
		fmt.Fprintf(&bild, "%s=%s", name, val)
	}
	return bild.String(), nil
}

func macdate(data []byte) string {
	t := binary.BigEndian.Uint32(data)
	if t == 0 {
		return "zero"
	} else {
		return time.Unix(int64(t)-2082844800, 0).UTC().Format("2006-01-02 15:04:05")
	}
}

func formatDates(data []byte) string {
	if len(data) < 16 {
		return "malformed " + hex.EncodeToString(data)
	}
	return fmt.Sprintf("(C=%s,M=%s,B=%s,A=%s)",
		macdate(data[:]),
		macdate(data[4:]),
		macdate(data[8:]),
		macdate(data[12:]))
}

func formatFinderInfo(data []byte) string {
	if len(data) < 32 {
		return "malformed " + hex.EncodeToString(data)
	}
	isDir := string(data[:4]) != "\x00\x00\x00\x00" && (data[0] < 32 || data[2] < 32)

	var bild strings.Builder
	if isDir {
		fmt.Fprintf(&bild, "(%d,%d,%d,%d) ",
			int16(binary.BigEndian.Uint16(data[0:2])),
			int16(binary.BigEndian.Uint16(data[2:4])),
			int16(binary.BigEndian.Uint16(data[4:6])),
			int16(binary.BigEndian.Uint16(data[6:8])))
	} else {
		fmt.Fprintf(&bild, "(%q,%q) ", data[:4], data[4:8])
	}

	bild.WriteByte('(')
	ff := binary.BigEndian.Uint16(data[8:])
	if ff&1 != 0 {
		bild.WriteString("isOnDesk,")
	}
	if ff&0xe != 0 {
		fmt.Fprintf(&bild, "color%d,", ff>>1&7)
	}
	if ff&0x10 != 0 {
		bild.WriteString("unknown0x10,")
	}
	if ff&0x20 != 0 {
		bild.WriteString("requireSwitchLaunch,")
	}
	if ff&0x40 != 0 {
		bild.WriteString("isShared,")
	}
	if ff&0x80 != 0 {
		bild.WriteString("hasNoINITs,")
	}
	if ff&0x100 != 0 {
		bild.WriteString("hasBeenInited,")
	}
	if ff&0x200 != 0 {
		bild.WriteString("aoceLetter,")
	}
	if ff&0x400 != 0 {
		bild.WriteString("hasCustomIcon,")
	}
	if ff&0x800 != 0 {
		bild.WriteString("isStationery,")
	}
	if ff&0x1000 != 0 {
		bild.WriteString("nameLocked,")
	}
	if ff&0x2000 != 0 {
		bild.WriteString("hasBundle,")
	}
	if ff&0x4000 != 0 {
		bild.WriteString("isInvisible,")
	}
	if ff&0x8000 != 0 {
		bild.WriteString("isAlias,")
	}

	fmt.Fprintf(&bild, ") (%d,%d) ", // location in the window
		int16(binary.BigEndian.Uint16(data[10:12])),
		int16(binary.BigEndian.Uint16(data[12:14])))

	rsrv := int16(binary.BigEndian.Uint16(data[14:16]))
	fmt.Fprintf(&bild, "(rsrv=%#x) ", rsrv)

	if string(data[16:32]) != string(make([]byte, 16)) {
		fmt.Fprintf(&bild, "(ext=%s) ", hex.EncodeToString(data[16:32]))
	}

	return strings.TrimSuffix(strings.ReplaceAll(bild.String(), ",)", ")"), " ")
}

func formatOtherInfo(data []byte) string {
	if len(data) != 4 || data[0]&0x3f != 0 || (data[1]|data[2]|data[3]) != 0 {
		return "malformed " + hex.EncodeToString(data)
	}
	var v []string
	if data[0]&0x80 != 0 {
		v = append(v, "locked")
	}
	if data[0]&0x40 != 0 {
		v = append(v, "protected")
	}
	return "(" + strings.Join(v, ",") + ")"
}
