package sit

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/elliotnunn/BeHierarchic/internal/appledouble"
	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
	"github.com/elliotnunn/BeHierarchic/internal/sectionreader"
)

type header struct {
	RAlgo, DAlgo AlgID
	NameLen      uint8
	NameField    [31]byte

	// Unexplored region
	_                            [16]byte
	FirstPtr, LastPtr, ParentPtr uint32
	_                            [4]byte

	// Truncated sadly
	FinderInfo [10]byte

	CrTime, ModTime uint32

	RUnpackLen, DUnpackLen uint32
	RPackLen, DPackLen     uint32
	RCRC, DCRC             uint16
}

func (h *header) Name() string {
	return strings.ReplaceAll(stringFromRoman(h.NameField[:min(31, h.NameLen)]), "/", ":")
}

type AlgID uint8

func (id AlgID) isDirStart() bool { return id == 32 }
func (id AlgID) isDirEnd() bool   { return id == 33 }

func oldFormat(fsys *fskeleton.FS, headerReader, dataReader io.ReaderAt, offset, filesize int64) {
	defer fsys.NoMore()
	type forlater struct {
		offset int64
		hdr    *header
	}
	var pass2 []forlater
	known := make(map[int64]*header)

	pathof := func(o int64) (string, bool) {
		var s string
		for o != 0 {
			hdr, ok := known[o]
			if !ok {
				return "", false
			}
			s = path.Join(hdr.Name(), s)
			o = int64(hdr.ParentPtr)
		}
		return s, true
	}

	handleRecord := func(offset int64, hdr *header) {
		if hdr.RAlgo.isDirEnd() {
			return
		}

		name, ok := pathof(offset)
		if !ok {
			pass2 = append(pass2, forlater{offset, hdr})
			return
		}

		var meta appledouble.AppleDouble
		meta.CreateTime = appledouble.MacTime(hdr.CrTime)
		meta.ModTime = appledouble.MacTime(hdr.ModTime)
		meta.Flags = binary.BigEndian.Uint16(hdr.FinderInfo[8:])

		if hdr.RAlgo.isDirStart() {
			fsys.Mkdir(name, fileID(offset, false), 0, meta.ModTime)
			adfile, adlen := meta.ForDir()
			fsys.CreateReader(appledouble.Sidecar(name), fileID(offset, true), adfile, adlen, 0, meta.ModTime)
		} else { // file
			copy(meta.Type[:], hdr.FinderInfo[:])
			copy(meta.Creator[:], hdr.FinderInfo[4:])
			rOffset := int64(offset + 112)
			if hdr.RAlgo == 0 {
				adfile, adsize := meta.WithResourceFork(io.NewSectionReader(dataReader, rOffset, int64(hdr.RUnpackLen)), int64(hdr.RUnpackLen))
				fsys.CreateReaderAt(appledouble.Sidecar(name),
					fileID(offset, true),
					adfile, adsize, 0, meta.ModTime)
			} else {
				adfile, adsize := meta.WithSequentialResourceFork(func() (io.ReadCloser, error) {
					raw := io.NewSectionReader(dataReader, rOffset, int64(hdr.RPackLen))
					return readerFor(hdr.RAlgo, "", hdr.RUnpackLen, hdr.RCRC, raw)
				}, int64(hdr.RUnpackLen))
				fsys.CreateReadCloser(appledouble.Sidecar(name),
					fileID(offset, true),
					adfile, adsize, 0, meta.ModTime)
			}

			dOffset := offset + 112 + int64(hdr.RPackLen)
			if hdr.DAlgo == 0 {
				fsys.CreateReaderAt(name,
					fileID(offset, false),
					sectionreader.Section(dataReader, dOffset, int64(hdr.DUnpackLen)), // readerAt
					int64(hdr.DUnpackLen), 0, meta.ModTime)
			} else {
				fsys.CreateReadCloser(name,
					fileID(offset, false),
					func() (io.ReadCloser, error) {
						raw := io.NewSectionReader(dataReader, dOffset, int64(hdr.DPackLen))
						return readerFor(hdr.DAlgo, "", hdr.DUnpackLen, hdr.DCRC, raw)
					}, // reader
					int64(hdr.DUnpackLen), 0, meta.ModTime)
			}
		}
	}

	for offset < filesize {
		hdrdata := make([]byte, 112)
		n, err := headerReader.ReadAt(hdrdata, offset)
		if n == len(hdrdata) { // ReadAt can return io.EOF on success if right at EOF
			err = nil
		}
		err = notExpectingEOF(err)
		if err == nil && calcCRC16(hdrdata[:110]) != binary.BigEndian.Uint16(hdrdata[110:]) {
			err = ErrHeader
		}

		if err != nil {
			slog.Warn("StuffIt read error", "err", err, "offset", offset)
			break
		}

		var hdr header
		binary.Read(bytes.NewReader(hdrdata), binary.BigEndian, &hdr)
		known[offset] = &hdr
		handleRecord(offset, &hdr)
		offset += 112
		if hdr.RAlgo&32 == 0 {
			offset += int64(hdr.RPackLen + hdr.DPackLen)
		}
	}

	// Second sweep, for the files that were encountered before their containing directory
	for _, rec := range pass2 {
		handleRecord(rec.offset, rec.hdr)
	}
}

// ReadAt returns ErrInvalid when offset > filesize
func notExpectingEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
