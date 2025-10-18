// Copyright (c) Elliot Nunn

// This library is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 2.1 of the License, or (at your option) any later version.

// This library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.

package sit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"testing/iotest"

	"github.com/elliotnunn/BeHierarchic/internal/fskeleton"
)

var (
	ErrFormat   = errors.New("not a StuffIt archive")
	ErrPassword = errors.New("password protected StuffIt archive")
	ErrAlgo     = errors.New("unimplemented StuffIt compression algorithm")
	ErrChecksum = errors.New("StuffIt checksum mismatch")
)

func New(disk io.ReaderAt) (fs.FS, error) {
	var (
		buf []byte
		err error
		r   = io.NewSectionReader(disk, 0, 200)
	)

	buf, err = creepTo(buf, r, 2)
	if err != nil || buf[0] != 'S' {
		return nil, eof2formaterr(err)
	}

	if buf[1] == 't' { // could be SIT version 5
		buf, err = creepTo(buf, r, 100)
		if err != nil {
			return nil, eof2formaterr(err)
		} else if string(buf[:16]) != "StuffIt (c)1997-" ||
			string(buf[20:80]) != " Aladdin Systems, Inc., http://www.aladdinsys.com/StuffIt/\r\n" {
			return nil, ErrFormat
		}
		hdrlen := int(binary.BigEndian.Uint16(buf[96:]))
		if hdrlen < 100 {
			return nil, ErrFormat
		}
		buf, err = creepTo(buf, r, hdrlen)
		if err != nil {
			return nil, eof2formaterr(err)
		}
		if !checkCRC16(buf, 98) {
			return nil, ErrChecksum
		}
		fsys := fskeleton.New()
		go newFormat(fsys, disk, int64(len(buf)))
		return fsys, nil
	} else {
		buf, err = creepTo(buf, r, 22)
		if err != nil {
			return nil, eof2formaterr(err)
		} else if string(buf[10:14]) != "rLau" {
			return nil, ErrFormat
		}
		// seems to be a CRC16 at offset 20 but I cannot get it to match... no matter
		fsys := fskeleton.New()
		go oldFormat(fsys, disk, int64(len(buf)))
		return fsys, nil
	}
}

func eof2formaterr(e error) error {
	if e == io.EOF {
		return ErrFormat
	} else {
		return e
	}
}

func creepTo(buf []byte, reader io.Reader, to int) ([]byte, error) {
	return creepBy(buf, reader, to-len(buf))
}

func creepBy(buf []byte, reader io.Reader, by int) ([]byte, error) {
	if by < 0 {
		return buf, errors.New("invalid structure length")
	}
	buf = slices.Grow(buf, by)
	n, err := io.ReadFull(reader, buf[len(buf):len(buf)+by])
	buf = buf[:len(buf)+n]
	switch err {
	case nil: //ok
		if n != by {
			panic("unreachable")
		}
		return buf, nil
	case io.ErrUnexpectedEOF, io.EOF:
		return buf, io.EOF
	default:
		return buf, err
	}
}

func readerFor(algo AlgID, crypto string, unpacksz uint32, cksum uint16, r io.Reader) io.ReadCloser {
	if crypto != "" {
		return io.NopCloser(iotest.ErrReader(ErrPassword))
	}

	// corpus includes algo 0, 2, 3, 5, 13, 15
	switch algo {
	case 0: // no compression
		return &crc16reader{r: io.NopCloser(r), len: int64(unpacksz), want: cksum}
	// case 1: // RLE compression
	case 2: // LZC compression
		return &crc16reader{r: lzc(r, unpacksz), len: int64(unpacksz), want: cksum}
	case 3: // Huffman compression
		return &crc16reader{r: huffman(r, unpacksz), len: int64(unpacksz), want: cksum}
	// case 5: // LZ with adaptive Huffman
	// case 6: // Fixed Huffman table
	// case 8: // Miller-Wegman encoding
	case 13: // anonymous
		return &crc16reader{r: sit13(r, unpacksz), len: int64(unpacksz), want: cksum}
	// case 14: // anonymous
	case 15: // Arsenic
		return arsenic(r, unpacksz) // has its own internal checksum
	default:
		return io.NopCloser(iotest.ErrReader(fmt.Errorf("%w: %d", ErrAlgo, algo)))
	}
}
