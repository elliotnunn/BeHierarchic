package sit

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

//go:embed stuffit-test-files/sources
var sourcesFS embed.FS

//go:embed stuffit-test-files/build
var archivesFS embed.FS

var algoTestCases = mkAlgoTestCases()

func TestAlgorithms(t *testing.T) {
	for _, x := range algoTestCases {
		if x.unpackedData == nil {
			continue // not much use at the moment, let's focus on the corpus
		}

		t.Run(x.String(), func(t *testing.T) {
			r := readerFor(algid(x.algorithm), x.unpackedSize, bytes.NewReader(x.packedData))
			got, goterr := io.ReadAll(r)
			if goterr != nil && goterr != io.EOF {
				t.Errorf("expected io.EOF or nil, got %v", goterr)
			}
			if len(got) != int(x.unpackedSize) {
				t.Errorf("expected %d bytes, got %d", x.unpackedSize, len(got))
			}

			if x.crc != 0 && x.algorithm != 15 { // compare CRC, but not for Arsenic
				t.Run("CRC", func(t *testing.T) {
					gotcrc := crc16(got)
					if gotcrc != x.crc {
						t.Errorf("expected CRC16 %04x, got %04x", x.crc, gotcrc)
					}
				})
			}

			if x.unpackedData != nil { // compare whole data
				t.Run("KnownData", func(t *testing.T) {
					if !bytes.Equal(got, x.unpackedData) && !sameTextFile(got, x.unpackedData) {
						t.Error(logMismatch(got, x.unpackedData, x.packedData, x.stuffitPath, x.path))
					}
				})
			}
		})
	}
}

type testCase struct {
	stuffitPath              string
	path                     string
	backing                  io.ReaderAt
	whichFork                string // "data"/"resource"
	algorithm                int8
	offset                   uint32
	packedSize               uint32
	unpackedSize             uint32
	crc                      uint16
	packedData, unpackedData []byte
}

func (t *testCase) String() string {
	algo := algoName(t.algorithm)
	if t.algorithm == 13 {
		if t.packedData[0]&0xf0 == 0 {
			algo += "dynamic"
			if t.packedData[0]&8 == 0 {
				algo += "2"
			} else {
				algo += "1"
			}
		} else {
			algo += "static"
		}
	}
	return fmt.Sprintf("%s/%s/%s/%cfork",
		algo,
		strings.ReplaceAll(t.stuffitPath, "/", ":"),
		strings.ReplaceAll(t.path, "/", ":"),
		t.whichFork[0])
}

func mkAlgoTestCases() []testCase {
	var ret []testCase
	known := fsToMap(sourcesFS)
	for sitPath, sitBytes := range fsToMap(archivesFS) {
		if strings.Contains(sitPath, "password") {
			continue // we really need a better way here
		}
		sit, err := New(bytes.NewReader(sitBytes))
		if err != nil {
			continue
		}

		fs.WalkDir(sit, ".", func(p string, d fs.DirEntry, err error) error {
			if d.IsDir() || strings.Contains(p, "._") {
				return nil
			}

			f, err := sit.Open(p)
			if err != nil {
				panic(err)
			}
			entry := f.(*openfile).s.e

			for i, fork := range entry.forks { // only data, we only care about tests
				c := testCase{
					stuffitPath:  sitPath,
					path:         p,
					whichFork:    [2]string{"data", "resource"}[i],
					algorithm:    int8(fork.algo),
					offset:       fork.packofst,
					packedSize:   fork.packsz,
					unpackedSize: fork.unpacksz,
					crc:          fork.crc,
					packedData:   sitBytes[fork.packofst:][:fork.packsz],
				}

				if knownDataFork, ok := known[path.Base(p)]; i == 0 && ok {
					c.unpackedData = knownDataFork
				}

				ret = append(ret, c)
			}
			return nil
		})
	}

	sort.Slice(ret, func(a, b int) bool {
		return ret[a].String() < ret[b].String()
	})

	return ret
}

func algoName(method int8) string {
	switch method {
	case 0:
		return "nocompress"
	case 1:
		return "RLE"
	case 2:
		return "LZC"
	case 3:
		return "Huffman"
	case 5:
		return "LZAH"
	case 6:
		return "FixedHuffman"
	case 8:
		return "LZMW"
	case 15:
		return "Arsenic"
	default:
		return fmt.Sprintf("SIT%d", method)
	}

}

func fsToMap(fsys fs.FS) map[string][]byte {
	nope := make(map[string]bool)
	ret := make(map[string][]byte)
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if nope[name] {
			return nil
		}
		if _, exist := ret[name]; exist {
			delete(ret, name)
			nope[name] = true
			return nil
		}
		ret[name], _ = fs.ReadFile(fsys, path)
		return nil
	})
	return ret
}

func safeName(s string) string {
	ok := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	var ret []byte
	for _, b := range []byte(s) {
		if bytes.ContainsRune(ok, rune(b)) {
			ret = append(ret, b)
		} else if !bytes.HasSuffix(ret, []byte(".")) {
			ret = append(ret, '.')
		}
	}
	return string(ret)
}

func logMismatch(got, expect, packed []byte, name1, name2 string) string {
	tmp := "/tmp"
	if runtime.GOOS == "windows" {
		tmp = `C:\Windows\Temp`
	}
	save := filepath.Join(tmp, safeName(name1), safeName(name2))
	os.MkdirAll(save, 0o755)
	os.WriteFile(filepath.Join(save, "expect"), expect, 0o644)
	os.WriteFile(filepath.Join(save, "got"), got, 0o644)
	os.WriteFile(filepath.Join(save, "packed"), packed, 0o644)
	return fmt.Sprintf("mismatched data logged: %s", filepath.Join(save, "{expect,got}"))
}

func sameTextFile(a, b []byte) bool {
	return bytes.Equal(
		bytes.ReplaceAll(a, []byte("\r"), []byte("\n")),
		bytes.ReplaceAll(b, []byte("\r"), []byte("\n")),
	)
}

var crctab [256]uint16

func init() {
	for i := range uint16(256) {
		k := i
		for range 8 {
			if k&1 != 0 {
				k = (k >> 1) ^ 0xa001
			} else {
				k >>= 1
			}
		}
		crctab[i] = k
	}
}

func crc16(buf []byte) uint16 {
	x := uint16(0)
	for _, c := range buf {
		x = crctab[byte(x)^c] ^ x>>8
	}
	return x
}
