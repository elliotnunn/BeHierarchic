package sit

import (
	"bytes"
	"embed"
	"errors"
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
			f, err := x.fsys.Open(x.path)
			if err != nil {
				panic("should have been able to open that")
			}
			defer f.Close()
			got, err := io.ReadAll(f)
			if errors.Is(err, ErrPassword) && strings.Contains(x.stuffitPath, "password") {
				t.Skipf("skipping password protected file")
			} else if errors.Is(err, ErrAlgo) {
				save := path.Join(tempFor(x.stuffitPath, x.path), "packed")
				os.WriteFile(save, x.packedData, 0o644)
				t.Fatalf("unimplemented algo: saved at %s", save)
			} else if err != nil {
				t.Fatalf("expected io.EOF or nil, got %v", err)
			}

			got = got[len(got)-int(x.fk.UnpackSize):]

			if len(got) != int(x.fk.UnpackSize) {
				t.Errorf("expected %d bytes, got %d", x.fk.UnpackSize, len(got))
			}

			if x.crc != 0 && x.fk.Algorithm != 15 { // compare CRC, but not for Arsenic
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
	fsys                     fs.FS
	path                     string
	whichFork                string // "data"/"resource"
	fk                       ForkDebug
	crc                      uint16
	packedData, unpackedData []byte
}

func (t *testCase) String() string {
	algo := algoName(t.fk.Algorithm)
	if t.fk.Algorithm == 13 {
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
		sit, err := New(bytes.NewReader(sitBytes))
		if err != nil {
			continue
		}

		fs.WalkDir(sit, ".", func(p string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				return nil
			}
			fork := "data"
			if strings.HasPrefix(path.Base(p), "._") {
				fork = "resource"
			}

			f, err := sit.Open(p)
			if err != nil {
				panic(err)
			}
			s, err := f.Stat()
			if err != nil {
				panic(err)
			}
			fk := s.Sys().(*ForkDebug)

			c := testCase{
				fsys:        sit,
				stuffitPath: sitPath,
				path:        p,
				whichFork:   fork,
				fk:          *fk,
				crc:         fk.CRC16,
				packedData:  sitBytes[fk.PackOffset:][:fk.PackSize],
			}

			if knownDataFork, ok := known[path.Base(p)]; fork == "data" && ok {
				c.unpackedData = knownDataFork
			}

			ret = append(ret, c)
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

func tempFor(name1, name2 string) string {
	tmp := "/tmp"
	if runtime.GOOS == "windows" {
		tmp = `C:\Windows\Temp`
	}
	save := filepath.Join(tmp, safeName(name1), safeName(name2))
	os.MkdirAll(save, 0o755)
	return save
}

func logMismatch(got, expect, packed []byte, name1, name2 string) string {
	save := tempFor(name1, name2)
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
