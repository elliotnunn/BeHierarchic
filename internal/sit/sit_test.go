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
			r := readerFor(byte(x.algorithm), bytes.NewReader(x.packedData), x.unpackedSize, x.String())
			got := make([]byte, x.unpackedSize)
			gotn, goterr := r.ReadAt(got, 0)
			got = got[:gotn]
			if goterr != nil && goterr != io.EOF {
				t.Errorf("expected io.EOF or nil, got %v", goterr)
			}
			if gotn != int(x.unpackedSize) {
				t.Errorf("expected %d bytes, got %d", x.unpackedSize, gotn)
			}
			if !bytes.Equal(got, x.unpackedData) && !sameTextFile(got, x.unpackedData) {
				t.Error(logMismatch(got, x.unpackedData, x.stuffitPath, x.path))
			}
		})
	}
}

type testCase struct {
	stuffitPath  string
	path         string
	whichFork    string // "data"/"resource"
	algorithm    int
	offset       int64
	packedSize   int64
	unpackedSize int64

	packedData, unpackedData []byte
}

func (t *testCase) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%#x+%#x->%#x",
		algoName(t.algorithm),
		strings.ReplaceAll(t.stuffitPath, "/", ":"),
		strings.ReplaceAll(t.path, "/", ":"),
		t.whichFork, t.offset, t.packedSize, t.unpackedSize)
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
			stat, err := f.Stat()
			if err != nil {
				panic(err)
			}
			forks := stat.Sys().([2]ForkDebug)

			for i, fork := range forks[:1] { // only data, we only care about tests
				if _, ok := known[path.Base(p)]; !ok {
					continue
				}
				ret = append(ret, testCase{
					stuffitPath:  sitPath,
					path:         p,
					whichFork:    [2]string{"data", "resource"}[i],
					algorithm:    fork.Algorithm,
					offset:       fork.PackOffset,
					packedSize:   fork.PackSize,
					unpackedSize: fork.UnpackSize,
					packedData:   sitBytes[fork.PackOffset:][:fork.PackSize],
					unpackedData: known[path.Base(p)],
				})
			}
			return nil
		})
	}

	sort.Slice(ret, func(a, b int) bool {
		return ret[a].String() < ret[b].String()
	})

	return ret
}

func algoName(method int) string {
	switch method {
	case 0: // no compression
		return "nocompress"
	case 3: // Huffman compression
		return "Huffman"
	case 1:
		return "RLE"
	case 2:
		return "LZC"
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

func logMismatch(got, expect []byte, name1, name2 string) string {
	save := filepath.Join(os.TempDir(), safeName(name1), safeName(name2))
	os.MkdirAll(save, 0o755)
	os.WriteFile(filepath.Join(save, "expect"), expect, 0o644)
	os.WriteFile(filepath.Join(save, "got"), got, 0o644)
	return fmt.Sprintf("mismatched data logged: %s", filepath.Join(save, "*"))
}

func sameTextFile(a, b []byte) bool {
	return bytes.Equal(
		bytes.ReplaceAll(a, []byte("\r"), []byte("\n")),
		bytes.ReplaceAll(b, []byte("\r"), []byte("\n")),
	)
}
