package flate

import (
	"errors"
	"io"
	"sort"
)

const (
	chunk = 1000000
)

type Reader struct {
	r           io.ReaderAt
	sm, lg      int64
	chunk       int
	checkpoints []resumePoint
	curcache    int
	seek        int64
}

func NewReader(r io.ReaderAt, smSize, lgSize int64) *Reader {
	return &Reader{
		r:  r,
		sm: smSize, lg: lgSize,
		checkpoints: make([]resumePoint, 1),
		chunk:       max(int(lgSize/5000), 500000),
		curcache:    -1,
	}
}

func (r *Reader) Size() int64 {
	return r.lg
}

func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= r.lg {
		return 0, io.EOF
	}
	endoff := min(r.lg, off+int64(len(p)))

	// Index of the first checkpoint that could satisfy this read
	i := sort.Search(len(r.checkpoints), func(i int) bool {
		return r.checkpoints[i].woffset > off
	}) - 1
	if i < 0 {
		panic("first checkpoint no good")
	}

	cursor := int64(0)
	for cursor < endoff {
		var err error
		if i != r.curcache { // cache is not sufficient
			if r.curcache >= 0 {
				r.checkpoints[r.curcache].thinOut()
			}
			r.curcache = i
			nrp, e := readAtLeast(r.r, r.sm, &r.checkpoints[i], r.chunk)
			err = e
			if i+1 == len(r.checkpoints) { // tells us how to get the next chunk
				r.checkpoints = append(r.checkpoints, nrp)
			}
		}

		usable := r.checkpoints[i].big[maxMatchOffset:]
		// This loop should be a conditional clipped copy()
		for j, b := range usable {
			is := r.checkpoints[i].woffset + int64(j)
			if is >= off && is < endoff {
				p[is-off] = b
				cursor = is + 1
			}
		}

		if cursor == endoff {
			err = io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return int(cursor - off), err // might be a harmless EOF or a real problem
		}
		i++
	}
	return int(cursor - off), nil
}

func (r *Reader) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.seek)
	r.seek += int64(n)
	return n, err
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.seek
	case io.SeekEnd:
		offset += r.lg
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
