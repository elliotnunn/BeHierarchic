// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package spinner

import (
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSimplest(t *testing.T) {
	fsys := new(fsys)
	id := reopenableFile{fsys, "fast4096"}

	buf := make([]byte, 4096)
	n, err := ReadAt(id, buf[:], 0)
	if n != 4096 || err != nil || !bufCorrect(0, buf) {
		t.Error(n, err, hex.EncodeToString(buf))
	}
}

func TestSpans(t *testing.T) {
	for _, fileSize := range []int{0, 1, 4094, 4095, 4096, 4097, 5000, 8092, 1000000} {
		for _, offset := range []int{-1, 0, 1, 4086, 4094, 4095, 4096, 4097, 5000, 999999} {
			for _, readSize := range []int{0, 1, 10, 4096, 8092} {
				fsys := new(fsys)
				id := reopenableFile{fsys, fmt.Sprintf("fast%d", fileSize)}

				expectN := readSize
				expectErr := error(nil)
				if offset < 0 {
					expectErr = fs.ErrInvalid
					expectN = 0
				} else if offset+readSize > fileSize {
					expectErr = io.EOF
					expectN = fileSize - offset
					expectN = max(0, expectN)
				}

				buf := make([]byte, readSize)
				gotN, gotErr := ReadAt(id, buf, int64(offset))

				if gotN != expectN || gotErr != expectErr || !bufCorrect(int64(offset), buf[:gotN]) {
					t.Errorf("ReadAt(fileSize=%d, readSize=%d, offset=%d) = (%d, %v) expected (%d, %v)",
						fileSize, readSize, offset, gotN, gotErr, expectN, expectErr)
				}
			}
		}
	}
}

func FuzzSpans(f *testing.F) {
	f.Fuzz(func(t *testing.T, fileSize int64, offset int64, readSize int) {
		if readSize < 0 {
			t.Skip()
		}
		fsys := new(fsys)
		id := reopenableFile{fsys, fmt.Sprintf("fast%d", fileSize)}

		expectN := readSize
		expectErr := error(nil)
		if offset < 0 {
			expectErr = fs.ErrInvalid
			expectN = 0
		} else if offset+int64(readSize) > fileSize {
			expectErr = io.EOF
			expectN = int(fileSize - offset)
			expectN = max(0, expectN)
		}

		buf := make([]byte, readSize)
		gotN, gotErr := ReadAt(id, buf, int64(offset))

		if gotN != expectN || gotErr != expectErr || !bufCorrect(int64(offset), buf[:gotN]) {
			t.Errorf("ReadAt(fileSize=%d, readSize=%d, offset=%d) = (%d, %v) expected (%d, %v)",
				fileSize, readSize, offset, gotN, gotErr, expectN, expectErr)
		}
	})
}

// func TestConcurrent(t *testing.T) {
// 	whereToRead := []struct {
// 		offset int64
// 		size   int
// 		got    int
// 		err    error
// 	}{
// 		{0, 10, 10, nil},
// 		{blockSize - 5, 5, 5, nil},
// 		{2*blockSize - 5, 5, 5, nil},
// 		{2*blockSize - 5, 4, 4, nil},
// 		{2*blockSize - 5, 10, 10, nil},
// 		{2*blockSize + 1, 10, 10, nil},
// 		{3*blockSize - 10, 10, 10, nil},
// 		{3*blockSize - 10, 12, 10, io.EOF},
// 		{3*blockSize + 0, 1, 0, io.EOF},
// 		{4*blockSize + 0, 1, 0, io.EOF},
// 	}

// 	fsys := new(fsys)
// 	f := reopenableFile{fsys, fmt.Sprintf("slow%d", 3*blockSize)}

// 	t.Run("parallelreads", func(t *testing.T) {
// 		for _, testCase := range whereToRead {
// 			t.Run(fmt.Sprintf("%d,%d", testCase.offset, testCase.size), func(t *testing.T) {
// 				t.Parallel()
// 				buf := make([]byte, testCase.size)
// 				n, err := ReadAt(f, buf, testCase.offset)
// 				if testCase.got != n {
// 					t.Errorf("wrong length! expected %d got %d", testCase.got, n)
// 				}
// 				if testCase.err != err {
// 					t.Errorf("wrong error! expected %v got %v", testCase.err, err)
// 				}
// 				ok := true
// 				for i, c := range buf[:n] {
// 					if c != byteAtOffset(testCase.offset+int64(i)) {
// 						ok = false
// 					}
// 				}
// 				if !ok {
// 					t.Error("data mismatch")
// 				}
// 			})
// 		}
// 	})

// 	t.Run("openCount", func(t *testing.T) {
// 		if fsys.openCount != 1 {
// 			t.Errorf("expected %d got %d", 1, fsys.openCount)
// 		}
// 	})
// }

// func TestSerial(t *testing.T) {
// 	whereToRead := [][]struct {
// 		offset int64
// 		size   int
// 		got    int
// 		err    error
// 	}{
// 		{
// 			{0, 10, 10, nil},
// 			{0, 10, 10, nil},
// 		},
// 		{
// 			{blockSize, 10, 10, nil},
// 			{0, 10, 10, nil},
// 			{blockSize * 2, 10, 10, nil},
// 		},
// 	}

// 	for i, group := range whereToRead {
// 		fsys := new(fsys)
// 		f := reopenableFile{fsys, fmt.Sprintf("fast%d", 3*blockSize)}

// 		t.Run(fmt.Sprintf("group%d", i), func(t *testing.T) {
// 			for _, testCase := range group {
// 				t.Run(fmt.Sprintf("%d,%d", testCase.offset, testCase.size), func(t *testing.T) {

// 					buf := make([]byte, testCase.size)
// 					n, err := ReadAt(f, buf, testCase.offset)
// 					if testCase.got != n {
// 						t.Errorf("wrong length! expected %d got %d", testCase.got, n)
// 					}
// 					if testCase.err != err {
// 						t.Errorf("wrong error! expected %v got %v", testCase.err, err)
// 					}
// 					ok := true
// 					for i, c := range buf[:n] {
// 						if c != byteAtOffset(testCase.offset+int64(i)) {
// 							ok = false
// 						}
// 					}
// 					if !ok {
// 						t.Error("data mismatch")
// 					}
// 				})
// 			}
// 		})
// 	}
// }

// func TestReadAhead(t *testing.T) {
// 	fsys := new(fsys)
// 	f := reopenableFile{fsys, fmt.Sprintf("slow%d", 2*blockSize)}

// 	ReadAt(f, make([]byte, 10), 0)
// 	time.Sleep(quantum) // wait to fill the cache with the next block

// 	start := time.Now()
// 	_, err := ReadAt(f, make([]byte, 10), blockSize)
// 	if time.Since(start) > quantum/4 {
// 		t.Error("lookahead negative")
// 	}
// 	if err != nil {
// 		t.Error(err)
// 	}
// }

type fsys struct {
	openCount int
	readLog   map[string]string
}

func (fsys *fsys) Open(name string) (fs.File, error) {
	slow := strings.HasPrefix(name, "slow")
	name = strings.TrimPrefix(name, "slow")
	name = strings.TrimPrefix(name, "fast")
	size, _ := strconv.Atoi(name)
	fsys.openCount++
	return &tediousReader{delay: slow, total: size}, nil
}

type reopenableFile struct {
	fsys     fs.FS
	filename string
}

func (r reopenableFile) Open() (fs.File, error) { return r.fsys.Open(r.filename) }
func (r reopenableFile) String() string         { return r.filename }

var quantum = time.Millisecond * 50

type tediousReader struct {
	fsys  fsys
	f     reopenableFile
	delay bool
	total int
	seek  int
}

// assumes a blocksize
func (r *tediousReader) Read(p []byte) (int, error) {
	if r.fsys.readLog == nil {
		r.fsys.readLog = make(map[string]string)
	}
	r.fsys.readLog[r.f.filename] = strings.TrimPrefix(fmt.Sprintf("%s %d", r.fsys.readLog[r.f.filename], r.seek), " ")
	for i := range p {
		if r.seek == r.total {
			return i, io.EOF
		}
		p[i] = byteAtOffset(int64(r.seek))
		r.seek++
	}
	if r.delay {
		time.Sleep(quantum)
	}
	return len(p), nil
}

func (r *tediousReader) Stat() (fs.FileInfo, error) { return r, nil }
func (r *tediousReader) Close() error               { return nil }
func (r *tediousReader) Size() int64                { return int64(r.total) }
func (r *tediousReader) Name() string               { return r.f.filename }
func (r *tediousReader) IsDir() bool                { return false }
func (r *tediousReader) Mode() fs.FileMode          { return 0 }
func (r *tediousReader) ModTime() time.Time         { return time.Time{} }
func (r *tediousReader) Sys() any                   { return nil }

func byteAtOffset(offset int64) byte { return byte(offset ^ offset>>8 ^ offset*5>>16) }

func bufCorrect(offset int64, buf []byte) bool {
	for i := range buf {
		if buf[i] != byteAtOffset(offset+int64(i)) {
			return false
		}
	}
	return true
}
