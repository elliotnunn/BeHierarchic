package main

import (
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"encoding/binary"
	"io"
	"io/fs"
	"math"
	gopath "path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/apm"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/hfs"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
	"github.com/elliotnunn/BeHierarchic/internal/sit"
	"github.com/elliotnunn/BeHierarchic/internal/tar"
	"github.com/therootcompany/xz"
)

const sizeUnknown = -1 // small negative numbers are most efficient for the disk cache

// probeArchive examines the filename and file header,
// and returns a function returning an fs.FS (which can be expensive to run).
//
// Much ink has been spilt over the problem of determining file types from examining headers.
// The competing requirements of this implementation are:
//   - Minimise seeking to the end of the file, which requires whole-file decompression if compressed
//   - Minimise querying the size of the file, which requires whole-file decompression if gzipped
//   - Leave enough header bytes in the SQLite cache that adding a new file format
//     might not require a very expensive update to every file's cache entry
//   - But also not fill up the cache needlessly
//   - Be sceptical of the file extension, only using it if it brings great savings
func (o path) probeArchive() (fsysGenerator, error) {
	info, err := o.rawStat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, err
	}

	headerReader, err := o.prefetchCachedOpen()
	if err != nil {
		return nil, err
	}
	dataReader := headerReader.withoutCaching()

	// Easy: switch on file extension
	switch gopath.Ext(o.name.Base()) {
	case ".tar":
		return func() (fs.FS, error) { return tar.New2(headerReader, dataReader), nil }, nil
	}

	// Slightly harder: switch on the first 16 bytes
	head := make([]byte, 16)
	n, err := headerReader.ReadAt(head, 0)
	if n != len(head) {
		if err == io.EOF {
			return nil, nil // not an archive
		} else if err != nil {
			return nil, err // an actual problem
		}
	}
	at := func(s string, o int) bool { return string(head[o:][:len(s)]) == s }

	switch {
	case at("StuffIt (c)1997-", 0) || at("S", 0) && at("rLau", 10):
		return func() (fs.FS, error) { return sit.New2(headerReader, dataReader) }, nil
	case at("ER", 0) && // Apple Partition Map
		(at("\x02\x00", 2) || at("\x04\x00", 2) || at("\x08\x00", 2) || at("\x10\x00", 2)): // block sizes
		return func() (fs.FS, error) {
			defer headerReader.stopCaching()
			return apm.New(headerReader)
		}, nil
	case at("\x1f\x8b\x08", 0):
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".gz .gzip .tgz=.tar")
			opener := func() (io.ReadCloser, error) {
				return gzip.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64))
			}
			fsys := fskeleton.New()
			fsys.CreateReadCloserFile(innerName, 0, opener, sizeUnknown, 0, info.ModTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case at("BZh", 0) && head[3] >= '0' && head[3] <= '9' && at("\x31\x41\x59\x26\x53\x59", 4) &&
		!strings.HasSuffix(o.name.Base(), ".dmg"): // UDIFs have a more complex format, ignore the bzip2 header
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".bz .bz2 .bzip2 .tbz=.tar .tb2=.tar")
			opener := func() (io.Reader, error) {
				return bzip2.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64)), nil
			}
			fsys := fskeleton.New()
			fsys.CreateReaderFile(innerName, 0, opener, sizeUnknown, 0, info.ModTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case at("\xfd7zXZ\x00", 0):
		return func() (fs.FS, error) {
			innerName := changeSuffix(o.name.Base(), ".xz .txz=.tar")
			opener := func() (io.Reader, error) {
				return xz.NewReader(io.NewSectionReader(dataReader, 0, math.MaxInt64), xz.DefaultDictMax)
			}
			fsys := fskeleton.New()
			fsys.CreateReaderFile(innerName, 0, opener, sizeUnknown, 0, info.ModTime(), nil)
			fsys.NoMore()
			return fsys, nil
		}, nil
	case at("MZ", 0): // possible self-extracting ZIP, work backward from end to find PK
		// currently only accommodates ZIP headers without a comment field
		stat, err := headerReader.Stat()
		if err != nil {
			return nil, err
		}
		size := stat.Size()

		if size >= 100 { // smallest conceivable self-extracting ZIP
			eocd := make([]byte, 22)
			n, err := headerReader.ReadAt(eocd, size-int64(len(eocd)))
			if n < len(eocd) {
				return nil, err
			}
			if string(eocd[:2]) == "PK" && string(eocd[20:]) == "\x00\x00" {
				goto zip
			}
		}
		break
	zip:
		fallthrough
	case at("PK\x03\x04", 0): // plain zip
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
			for _, f := range r.File { // hack to make zips fast
				if strings.HasSuffix(f.Name, "/") {
					continue
				}
				ofs, err := f.DataOffset() // get all the metadata we need to read the archive
				if err != nil {
					continue
				}
				o.container.zMu.Lock()
				if o.container.zipLocs == nil {
					o.container.zipLocs = make(map[path]int64)
				}
				o.container.zipLocs[path{o.container, r, internpath.New(f.Name)}] = ofs
				o.container.zMu.Unlock()
			}
			return r, nil
		}, nil
	}

	// Hardest: HFS volumes
	// - has no reliable file extension or type code
	// - magic number offset by 1 kb
	// - (unsupported) Disk Copy compression leaves the magic number intact
	// First two bytes of the "boot block" will be blank or Larry Kenyon's initials
	if at("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00", 0) || // boot blocks truly empty
		at("LK\x60", 0) || // boot blocks on
		at("\x00\x00\x60", 0) { // boot blocks deliberately disabled
		stat, err := o.cookedStat()
		if err != nil {
			return nil, err
		}
		size := stat.Size()
		if size >= 400*1024 { // smallest Mac floppy
			mdb := make([]byte, 128)
			n, _ := headerReader.ReadAt(mdb, 1024)
			drAlBlkSiz := binary.BigEndian.Uint32(mdb[0x14:])
			if n == len(mdb) &&
				string(mdb[:2]) == "BD" && string(mdb[0x7c:0x7e]) != "H+" && // enforce HFS, exclude HFS+ wrapper
				drAlBlkSiz >= 512 && drAlBlkSiz%512 == 0 { // reinforce the fairly weak magic number
				return func() (fs.FS, error) { return hfs.New2(headerReader, dataReader) }, nil
			}
		}
	}
	headerReader.Close()
	return nil, nil // not an archive
}

func changeSuffix(s string, suffixes string) string {
	for _, rule := range strings.Split(suffixes, " ") {
		from, to, _ := strings.Cut(rule, "=")
		if strings.HasSuffix(s, "_"+from) && len(s) > len(from)+1 {
			// Apache sometimes munges "file.tar.gz" to "file.tar_.gz" on upload
			return s[:len(s)-len(from)-1] + to
		} else if strings.HasSuffix(s, from) && len(s) > len(from) {
			return s[:len(s)-len(from)] + to
		}
	}
	return s
}
