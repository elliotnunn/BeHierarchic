package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

func dumpFS(fsys fs.FS) {
	const tfmt = "2006-01-02T15:04:05"
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		fmt.Printf("%#v\n", p)
		if d == nil {
			fmt.Println("    nil info!")
			return nil
		}

		i, err := d.Info()
		if err != nil {
			panic(err)
		}

		fmt.Printf("    %v size=%d modtime=%s\n",
			i.Mode(), i.Size(), i.ModTime().Format(tfmt))

		// AppleDouble file
		if strings.HasPrefix(path.Base(p), "._") {
			f, err := fsys.Open(p)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			fmt.Println(dumpAppleDouble(f))
		}

		return nil
	})
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

func dumpAppleDouble(r io.Reader) (string, error) {
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

func main() {
	base := os.Args[1]
	concrete := os.DirFS(base)
	abstract := Wrapper(concrete)

	go func() {
		dumpFS(concrete)
		fmt.Println("----")
		dumpFS(abstract)
	}()
	http.ListenAndServe(":1993", http.FileServerFS(abstract))
}
