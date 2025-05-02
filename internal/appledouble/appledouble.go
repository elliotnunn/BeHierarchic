package appledouble

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/elliotnunn/resourceform/internal/multireaderat"
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

// Synth AppleDouble sidecar file from provided info

func Make(shortRecs map[int][]byte, longRecs map[int]multireaderat.SizeReaderAt) multireaderat.SizeReaderAt {
	var k1 []int
	for k := range shortRecs {
		k1 = append(k1, k)
	}
	slices.Sort(k1)
	var k2 []int
	for k := range longRecs {
		k1 = append(k1, k)
	}
	slices.Sort(k2)
	keys := append(k1, k2...)

	buf := make([]byte, 26+12*len(keys))
	copy(buf, "\x00\x05\x16\x00\x00\x02\x00\x00")
	binary.BigEndian.PutUint16(buf[24:], uint16(len(keys)))

	offset := uint32(len(buf))
	mrlist := []multireaderat.SizeReaderAt{nil}
	for i, key := range keys {
		recOffset := 26 + 12*i
		binary.BigEndian.PutUint32(buf[recOffset:], uint32(key))
		binary.BigEndian.PutUint32(buf[recOffset+4:], offset)

		var size uint32
		if r, isLongReader := longRecs[key]; isLongReader {
			size = uint32(r.Size())
			if size > 0 {
				mrlist = append(mrlist, r)
			}
		} else {
			d := shortRecs[key]
			size = uint32(len(d))
			buf = append(buf, d...)
		}
		binary.BigEndian.PutUint32(buf[recOffset+8:], size)
		offset += size
	}
	mrlist[0] = bytes.NewReader(buf)

	if len(mrlist) == 1 {
		return mrlist[0]
	} else {
		return multireaderat.New(mrlist...)
	}
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
		return "", fmt.Errorf("truncated appledouble: %w", err)
	}
	buf = buf[:n]

	buf[3] = 0x00 // is 7 in other implementations??
	if string(buf[:8]) != "\x00\x05\x16\x00\x00\x02\x00\x00" {
		return "", errors.New("not an appledouble" + hex.EncodeToString(buf[:8]))
	}

	count := binary.BigEndian.Uint16(buf[24:])
	s := ""
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
				val = hex.EncodeToString(data)
			case 9: // FINDER_INFO
				val = hex.EncodeToString(data)
			case 10: // MACINTOSH_FILE_INFO
				val = hex.EncodeToString(data)
			}
		}
		s += name + "=" + val + "\n"
	}
	return s, nil
}
