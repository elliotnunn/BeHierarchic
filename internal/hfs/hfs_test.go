// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package hfs

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
)

// - manyExtents has two files, each with many extents in the overflow file,
//   created by alternately pasting into them using TeachText
// - complex is a Mac OS 9.2.2 installation with the file contents zeroed

//go:embed testimg
var testImagesFS embed.FS
var testImages = make(map[string][]byte)

// Make a map of images, unzipping as necessary
func init() {
	fs.WalkDir(testImagesFS, ".", func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			var r io.Reader
			r, _ = testImagesFS.Open(path)
			name, zipped := strings.CutSuffix(d.Name(), ".gz")
			if zipped {
				r, _ = gzip.NewReader(r)
			}
			testImages[strings.TrimSuffix(name, ".img")], _ = io.ReadAll(r)
		}
		return nil
	})
}

func TestFS(t *testing.T) {
	f := bytes.NewReader(testImages["complex"])

	fsys, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(fsys, "Macintosh HD/System Folder/System")
	if err != nil {
		t.Fatal(err)
	}
}

func TestManyExtents(t *testing.T) {
	f := bytes.NewReader(testImages["manyExtents"])

	fsys, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(fsys, "ManyExtents/File2")
	if err != nil {
		t.Fatal(err)
	}

	for _, fname := range []string{"ManyExtents/File1", "ManyExtents/File2"} {
		f2, err := fsys.Open(fname)
		if err != nil {
			t.Fatal(err)
		}

		d, err := io.ReadAll(f2)
		if err != nil {
			t.Fatal(err)
		}

		if len(d) != 31*1024 {
			t.Fatalf("Expected %s to be 31k, not %d", fname, len(d))
		}

		for _, ch := range d {
			if ch != 0x20 {
				t.Fatalf("Expected %s to contain only spaces", fname)
			}
		}
	}
}

func TestComplex(t *testing.T) {
	f := bytes.NewReader(testImages["complex"])

	fsys, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		f, err := fsys.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}

		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}

		// The test image zeroes out every fork except the last byte
		expectLastByte := byte(stat.Sys().(uint32)) // Last byte of fork = low byte of CNID
		if strings.Contains("/"+p, "/._") {
			data = appleDoubleResFork(data)
			expectLastByte = ^expectLastByte // But ones-complement for resource forks
		}
		if len(data) > 0 && data[len(data)-1] != expectLastByte {
			t.Errorf("%s: last byte expected %#02x got %#02x",
				p, expectLastByte, data[len(data)-1])
		}

		return nil
	})
}

func BenchmarkNew(b *testing.B) {
	data := testImages["complex"]

	for _, action := range []string{"Parse", "ReadFilesFrom", "ParallelReadFilesFrom"} {
		for _, src := range []string{"InFile", "InRAM"} {
			b.Run(action+"Image"+src, func(pb *testing.B) {
				var r io.ReaderAt
				switch src {
				case "InFile":
					f, err := os.CreateTemp("", "hfstest.img")
					if err != nil {
						pb.Fatal(err)
					}
					defer os.Remove(f.Name())

					f.Write(data)
					f.Seek(0, io.SeekStart)
					r = f

				case "InRAM":
					r = bytes.NewReader(data)
				}

				switch action {
				case "Parse":
					pb.ResetTimer()
					for i := 0; i < pb.N; i++ {
						_, err := New(r)
						if err != nil {
							pb.Fatal(err)
						}
					}
				case "ReadFilesFrom", "ParallelReadFilesFrom":
					fsys, err := New(r)
					if err != nil {
						pb.Fatal(err)
					}

					workers := 1
					if strings.HasPrefix(action, "Parallel") {
						workers = 16
					}

					var wg sync.WaitGroup
					ch := make(chan string)

					for i := 0; i < workers; i++ {
						go func() {
							for path := range ch {
								f, _ := fsys.Open(path)
								io.ReadAll(f)
								wg.Done()
							}
						}()
					}

					pb.ResetTimer()

					for i := 0; i < pb.N; i++ {
						fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
							if !d.IsDir() {
								wg.Add(1)
								ch <- path
							}
							return nil
						})
						wg.Wait()
					}
				}
			})
		}
	}
}

func appleDoubleResFork(buf []byte) []byte {
	if string(buf[:3]) != "\x00\x05\x16" || string(buf[4:8]) != "\x00\x02\x00\x00" {
		return nil // not a valid appleDouble
	}

	count := binary.BigEndian.Uint16(buf[24:])
	for i := range count {
		kind := binary.BigEndian.Uint32(buf[26+12*i:])
		offset := binary.BigEndian.Uint32(buf[26+12*i+4:])
		size := binary.BigEndian.Uint32(buf[26+12*i+8:])
		if kind == appledouble.RESOURCE_FORK {
			return buf[offset:][:size]
		}

	}
	return nil
}
