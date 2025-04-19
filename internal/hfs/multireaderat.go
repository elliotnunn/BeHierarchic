package hfs

import (
	"errors"
	"io"
)

type multiReaderAt struct {
	backing io.ReaderAt
	extents []int64
	seek    int64
}

func (r *multiReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, io.EOF
	}

	lenbuf := int64(len(p))
	n := int64(0)
	extents := r.extents
	seek := int64(0)

	// Skip uninvolved extents
	for len(extents) > 0 && seek+extents[1] <= off {
		seek += seek + extents[1]
		extents = extents[2:]
	}
	if len(extents) == 0 {
		return 0, io.EOF
	}

	for n < lenbuf && len(extents) > 0 {
		disx, disn := extents[0], extents[1]
		if seek < off {
			disx += off - seek
			disn -= off - seek
			seek = off
		}
		disn = min(disn, off+lenbuf-seek)
		_, err := r.backing.ReadAt(p[seek-off:], disx)
		if err != nil {
			panic("could not read the bytes I expected!")
		}
		seek += disn
		n += disn
		extents = extents[2:]
	}

	if n < lenbuf {
		return int(n), io.EOF
	} else {
		return int(n), nil
	}
}

func (r *multiReaderAt) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.seek)
	r.seek += int64(n)
	return n, err
}

func (r *multiReaderAt) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.seek
	case io.SeekEnd:
		for i := 0; i < len(r.extents); i += 2 { // count my size, can't remember
			offset += r.extents[i+1]
		}
	default:
		return 0, errWhence
	}
	if offset < 0 {
		return 0, errOffset
	}
	r.seek = offset
	return offset, nil
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")
