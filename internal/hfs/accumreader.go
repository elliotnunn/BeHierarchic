package hfs

import (
	"io"
	"slices"
)

func newAccumReader(r io.ReaderAt) io.ReaderAt {
	return &accumReader{r: r}
}

type accumReader struct {
	r      io.ReaderAt
	buffer []byte
}

// ReadAt must not be called from multiple goroutines,
// will return 0 bytes for attempted read that exceeds EOF
func (r *accumReader) ReadAt(p []byte, off int64) (int, error) {
	need := off + int64(len(p))

	if need < 0 || need > 0x100000000 {
		return 0, io.EOF
	} else if need > int64(len(r.buffer)) {
		r.buffer = slices.Grow(r.buffer, int(need)-len(r.buffer))

		n, err := r.r.ReadAt(r.buffer[len(r.buffer):need], int64(len(r.buffer)))
		if n != int(need)-len(r.buffer) {
			return 0, err
		}
		r.buffer = r.buffer[:need]
	}

	return copy(p, r.buffer[off:need]), nil
}
