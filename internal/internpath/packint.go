package internpath

import (
	"encoding/binary"
	"math/bits"
)

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Pack with a prefix code
func putg[T integer](n T) {
	glomp := uint64(n)
	switch bits.Len64(glomp) {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		glomp <<= 1
		array[bump] = uint8(glomp)
		bump += 1
	case 8, 9, 10, 11, 12, 13, 14:
		glomp <<= 2
		glomp |= 0b01
		binary.LittleEndian.PutUint16(array[bump:], uint16(glomp))
		bump += 2
	case 15, 16, 17, 18, 19, 20, 21:
		glomp <<= 3
		glomp |= 0b011
		binary.LittleEndian.PutUint32(array[bump:], uint32(glomp))
		bump += 3
	case 22, 23, 24, 25, 26, 27, 28:
		glomp <<= 4
		glomp |= 0b0111
		binary.LittleEndian.PutUint32(array[bump:], uint32(glomp))
		bump += 4
	case 29, 30, 31, 32, 33, 34, 35, 36:
		glomp <<= 4
		glomp |= 0b1111
		binary.LittleEndian.PutUint64(array[bump:], glomp)
		bump += 5
	default:
		panic("not able to store such large integers")
	}
}

func get[T integer](from []byte) ([]byte, T) {
	glomp := binary.LittleEndian.Uint64(from)
	switch glomp & 0b1111 {
	case 0b0000, 0b1000, 0b0100, 0b1100, 0b0010, 0b1010, 0b0110, 0b1110:
		from = from[1:]
		glomp &= 0xff
		glomp >>= 1 // 7 bits
	case 0b0001, 0b1001, 0b0101, 0b1101:
		from = from[2:]
		glomp &= 0xffff
		glomp >>= 2 // 14 bits
	case 0b0011, 0b1011:
		from = from[3:]
		glomp &= 0xffffff
		glomp >>= 3 // 21 bits
	case 0b0111:
		from = from[4:]
		glomp &= 0xffffffff
		glomp >>= 4 // 28 bits
	case 0b1111:
		from = from[5:]
		glomp &= 0xffffffffff
		glomp >>= 4 // 36 bits
	}
	return from, T(glomp)
}
