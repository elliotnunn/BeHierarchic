// Copyright Elliot Nunn. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tar

import (
	"io"
	"math"

	"github.com/elliotnunn/BeHierarchic/internal/sectionreader"
)

func readerFromSparseHoles(r io.ReaderAt, physStart, physLength int64, sph sparseHoles) (io.ReaderAt, int64) {
	if len(sph) == 0 {
		return sectionreader.Section(r, physStart, physLength), physLength
	}

	var x []extent
	var log, phys int64
	for _, hole := range sph {
		dataLength := hole.Offset - log
		if dataLength > 0 {
			x = append(x, extent{log: log, phys: physStart + phys, length: dataLength})
		}
		log = hole.Offset
		phys += dataLength

		x = append(x, extent{log: log, phys: math.MinInt64, length: hole.Length})
		log = hole.endOffset()
	}
	lastDataLength := phys - physLength
	if lastDataLength > 0 {
		x = append(x, extent{log: log, phys: physStart + phys, length: lastDataLength})
		log += lastDataLength
	}

	return &sparseReader{r: r, extents: x}, log
}

type sparseReader struct {
	r       io.ReaderAt
	extents []extent
}

type extent struct {
	log, phys, length int64
}

func (r *sparseReader) ReadAt(p []byte, off int64) (n int, err error) {
	for _, x := range r.extents {
		x.log -= off // x.log becomes an offset into []byte

		if x.log+x.length <= 0 { // linear search, unlikely to slow us down much
			continue
		} else if x.log >= int64(len(p)) {
			break
		}

		if x.log < 0 { // trim left
			shortenLeft := -x.log
			x.phys += shortenLeft
			x.length -= shortenLeft
			x.log = 0
		}
		if x.log+x.length > int64(len(p)) { // trim right
			x.length = int64(len(p)) - x.log
		}

		if x.phys < 0 { // "hole"
			clear(p[x.log:][:x.length])
			n += int(x.length)
		} else { // "data"
			var subN int
			subN, err = r.r.ReadAt(p[x.log:][:x.length], x.phys)
			n += subN
			if err == nil {
				break
			}
		}
	}
	if n == len(p) {
		return n, nil
	} else {
		return n, err
	}
}
