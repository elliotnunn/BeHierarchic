package main

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/fs"
	"math"
	"strings"
	"testing/iotest"
	"time"

	"github.com/elliotnunn/BeHierarchic/internal/apm"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/hfs"
	"github.com/elliotnunn/BeHierarchic/internal/sit"
	"github.com/elliotnunn/BeHierarchic/internal/tar"
	"github.com/therootcompany/xz"
)

const sizeUnknown = -1 // small negative numbers are most efficient for the disk cache

func (o path) probeArchive() (fsysGenerator, error) {
	headerReader, err := o.prefetchCachedOpen()
	if err != nil {
		return nil, err
	}
	dataReader := headerReader.withoutCaching()

	// read the bare minimum of bytes required to answer the question
	cache := make(map[int]byte) // a little bit ick
	// but it serves to make clear which bytes we are making our decision on
	byteAt := func(offset int) int {
		got, ok := cache[offset]
		if !ok {
			var buf [1]byte
			n, _ := headerReader.ReadAt(buf[:], int64(offset))
			if n == 0 {
				return -1
			}
			cache[offset] = buf[0]
			got = buf[0]
		}
		return int(got)
	}
	matchAt := func(s string, offset int) bool {
		for i, c := range []byte(s) {
			if byteAt(offset+i) != int(c) {
				return false
			}
		}
		return true
	}

	getTime := func() time.Time {
		s, err := headerReader.Stat()
		if err != nil {
			return time.Time{}
		}
		return s.ModTime()
	}

	switch {
	case matchAt("\x1f\x8b", 0): // gzip
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".gz .gzip .tgz=.tar")
			opener := func() io.Reader {
				r, err := gzip.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64))
				if err != nil {
					return iotest.ErrReader(err)
				}
				return r
			}
			fsys := fskeleton.New()
			fsys.CreateSequentialFile(innerName, 0, opener, sizeUnknown, 0, getTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case matchAt("BZ", 0): // bzip2
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".bz .bz2 .bzip2 .tbz=.tar .tb2=.tar")
			opener := func() io.Reader {
				return bzip2.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64))
			}
			fsys := fskeleton.New()
			fsys.CreateSequentialFile(innerName, 0, opener, sizeUnknown, 0, getTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case matchAt("\xfd7zXZ\x00", 0): // xz
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".xz .txz=.tar")
			opener := func() io.Reader {
				r, err := xz.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64), xz.DefaultDictMax)
				if err != nil {
					return iotest.ErrReader(err)
				}
				return r
			}
			fsys := fskeleton.New()
			fsys.CreateSequentialFile(innerName, 0, opener, sizeUnknown, 0, getTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case matchAt("ER", 0): // Apple Partition Map
		return func() (fs.FS, error) {
			defer headerReader.stopCaching()
			return apm.New(headerReader)
		}, nil
	case matchAt("PK", 0): // Zip file // ... essential that we get the size sorted out...
		stat, err := headerReader.Stat()
		if err != nil {
			return nil, err
		}
		size := stat.Size()
		return func() (fs.FS, error) {
			defer headerReader.stopCaching()
			r, err := zip.NewReader(headerReader, size)
			if err != nil {
				return nil, err
			}
			for _, f := range r.File {
				f.DataOffset() // get all the metadata we need to read the archive
			}
			return r, nil
		}, nil
	case matchAt("StuffIt (c)1997-", 0) || matchAt("S", 0) && matchAt("rLau", 10):
		return func() (fs.FS, error) { return sit.New2(headerReader, dataReader) }, nil
	case matchAt("ustar\x00\x30\x30", 257) || matchAt("ustar\x20\x20\x00", 257): // posix tar
		return func() (fs.FS, error) { return tar.New2(headerReader, dataReader), nil }, nil
	case (matchAt("LK", 0) || matchAt("\x00\x00", 0)) && matchAt("BD", 1024): // don't want to read a whole KB!
		return func() (fs.FS, error) { return hfs.New2(headerReader, dataReader) }, nil
	}
	headerReader.Close()
	return nil, nil // not an archive
}

func changeSuffix(s string, suffixes string) string {
	for _, rule := range strings.Split(suffixes, " ") {
		from, to, _ := strings.Cut(rule, "=")
		if strings.HasSuffix(s, from) && len(s) > len(from) {
			return s[:len(s)-len(from)] + to
		}
	}
	return s
}
