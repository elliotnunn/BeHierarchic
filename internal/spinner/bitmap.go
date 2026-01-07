// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package spinner

import (
	"math/bits"
)

type bitmap struct {
	size   int
	nclear int
	data   []uint
	inline [1]uint
}

func newBitmap(size int) bitmap {
	if size < 0 {
		panic("negative bit count")
	}
	b := bitmap{size: size, nclear: size}
	if size > bits.UintSize {
		b.data = make([]uint, (size+bits.UintSize-1)/bits.UintSize)
	}
	return b
}

func (m *bitmap) set(idx int) {
	if idx < 0 {
		panic("negative bit index")
	}
	data := m.data
	if data == nil {
		data = m.inline[:]
	}
	mask := uint(1) << (idx % bits.UintSize)
	if data[idx/bits.UintSize]&mask == 0 {
		data[idx/bits.UintSize] |= mask
		m.nclear--
	}
}

func (m *bitmap) firstClear(fromIdx int) int {
	if fromIdx < 0 {
		panic("negative bit index")
	}
	data := m.data
	if data == nil {
		data = m.inline[:]
	}
	for idx := fromIdx; idx < m.size; idx++ {
		mask := uint(1) << (idx % bits.UintSize)
		if data[idx/bits.UintSize]&mask == 0 {
			return idx
		}
	}
	return -1
}
