// Copyright Elliot Nunn. Portions copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import (
	"hash"
	"hash/crc32"
	"io"
	"sync"
)

// newChecksumReaderAt wraps an [io.Reader]/[io.ReadCloser] and checks the CRC32.
func newChecksumReader(r io.Reader, size int64, checksum uint32) io.ReadCloser {
	rc, ok := r.(io.ReadCloser)
	if !ok {
		rc = io.NopCloser(r)
	}
	return &checksumReader{rc: rc, remain: size, sum: checksum, hash: crc32.NewIEEE()}
}

type checksumReader struct {
	rc     io.ReadCloser
	remain int64
	sum    uint32
	hash   hash.Hash32 // nil means hash check failed
}

func (r *checksumReader) Read(b []byte) (n int, err error) {
	if r.hash == nil {
		return 0, ErrChecksum
	}
	n, err = r.rc.Read(b)
	r.hash.Write(b[:n])
	r.remain -= int64(n)
	if r.remain == 0 && r.sum != 0 && r.hash.Sum32() != r.sum {
		r.hash = nil
		return n, ErrChecksum
	}
	return
}

func (r *checksumReader) Close() error { return r.rc.Close() }

// newChecksumReaderAt wraps an [io.ReaderAt] so that, if read from start to finish,
// it will check the CRC32, despite this being an awkward thing to do.
func newChecksumReaderAt(r io.ReaderAt, size int64, checksum uint32) io.ReaderAt {
	return &checksumReaderAt{r: r, size: size, sum: checksum, hash: crc32.NewIEEE()}
}

type checksumReaderAt struct {
	mu       sync.Mutex
	r        io.ReaderAt
	size     int64
	progress int64
	sum      uint32      // if zero then all is well
	hash     hash.Hash32 // nil means hash check complete
}

func (r *checksumReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = r.r.ReadAt(p, off)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hash != nil { // still hashing, try to absorb this data
		if off <= r.progress && off+int64(n) > r.progress {
			r.hash.Write(p[r.progress-off:])
			r.progress = off + int64(n)
		}

		if r.progress == r.size {
			if r.hash.Sum32() == r.sum {
				r.sum = 0
			}
			r.hash = nil
		}
	}

	if r.hash == nil && r.sum != 0 && off+int64(n) == r.size {
		err = ErrChecksum
	}
	return n, err
}
