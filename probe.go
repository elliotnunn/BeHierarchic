package main

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
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

func (fsys *FS) probeArchive(subsys fs.FS, subname string) (fsysGenerator, error) {
	f, err := subsys.Open(subname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var header []byte
	var accessError error
	matchAt := func(s string, offset int) bool {
		if len(header) < offset+len(s) && len(header) == cap(header) {
			target := (offset + len(s) + 63) &^ 63
			header = slices.Grow(header, target-len(header))
			n, err := io.ReadFull(f, header[len(header):cap(header)])
			if err != nil && err != io.EOF && accessError == nil {
				accessError = err
			}
			header = header[:len(header)+n]
		}
		return len(header) >= offset+len(s) && string(header[offset:][:len(s)]) == s
	}

	getTime := func() time.Time {
		s, err := f.Stat()
		if err != nil {
			return time.Time{}
		}
		return s.ModTime()
	}

	switch {
	case matchAt("\x1f\x8b", 0): // gzip
		return func(r io.ReaderAt) (fs.FS, error) {
			innerName := changeSuffix(path.Base(subname), ".gz .gzip .tgz=.tar")
			opener := func() io.Reader {
				r, err := gzip.NewReader(io.NewSectionReader(r, 0, math.MaxInt64))
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
		return func(r io.ReaderAt) (fs.FS, error) {
			innerName := changeSuffix(path.Base(subname), ".bz .bz2 .bzip2 .tbz=.tar .tb2=.tar")
			opener := func() io.Reader {
				return bzip2.NewReader(io.NewSectionReader(r, 0, math.MaxInt64))
			}
			fsys := fskeleton.New()
			fsys.CreateSequentialFile(innerName, 0, opener, sizeUnknown, 0, getTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case matchAt("\xfd7zXZ\x00", 0): // xz
		return func(r io.ReaderAt) (fs.FS, error) {
			innerName := changeSuffix(path.Base(subname), ".xz .txz=.tar")
			opener := func() io.Reader {
				r, err := xz.NewReader(io.NewSectionReader(r, 0, math.MaxInt64), xz.DefaultDictMax)
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
		return func(r io.ReaderAt) (fs.FS, error) { return apm.New(r) }, nil
	case matchAt("PK", 0): // Zip file // ... essential that we get the size sorted out...
		s, err := fsys.tryToGetSize(subsys, subname)
		if err != nil {
			return nil, err
		}
		return func(r io.ReaderAt) (fs.FS, error) { return zip.NewReader(r, s) }, nil
	case matchAt("rLau", 10) || matchAt("StuffIt (c)1997-", 0):
		return func(r io.ReaderAt) (fs.FS, error) { return sit.New(r) }, nil
	case matchAt("ustar\x00\x30\x30", 257), matchAt("ustar\x20\x20\x00", 257): // posix tar
		return func(r io.ReaderAt) (fs.FS, error) { return tar.New(r), nil }, nil
	case matchAt("BD", 1024):
		return func(r io.ReaderAt) (fs.FS, error) { return hfs.New(r) }, nil
	}
	return nil, accessError
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
