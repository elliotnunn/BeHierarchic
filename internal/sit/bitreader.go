package sit

import (
	"io"
	"math/bits"
)

const (
	InitialLittleEndian int  = 1
	InitialBigEndian    uint = 1 << (bits.UintSize - 1)
)

func FillLittleEndian(bbuf int, r io.ByteReader) int {
	// leadingzeros + 1 + goodbits = UintSize
	// unless leadingzeros = 0, in which case we don't know goodbits
	lz := bits.LeadingZeros(uint(bbuf))
	if lz <= 9 {
		return bbuf
	}

	goodbits := bits.UintSize - lz - 1
	bbuf &= ^(1 << goodbits) // clear the marker bit
	for {
		bite, err := r.ReadByte()
		if err != nil {
			break
		}
		bbuf |= int(bite) << goodbits
		goodbits += 8
		if goodbits+10 > bits.UintSize {
			break
		}
	}
	bbuf |= 1 << goodbits // replace the marker bit
	return bbuf
}

// This one is slightly trickier because we can't use the right-shift-sign-bit trick
func FillBigEndian(bbuf uint, r io.ByteReader) uint {
	// goodbits + 1 + trailingzeros = UintSize
	// unless leadingzeros = 0, in which case we don't know goodbits
	tz := bits.TrailingZeros(bbuf)
	if tz < 8 || tz == bits.UintSize {
		return bbuf
	}

	bbuf &= ^(1 << tz) // clear the marker bit
	for {
		bite, err := r.ReadByte()
		if err != nil {
			break
		}
		tz -= 8
		bbuf |= uint(bite) << (tz + 1)
		if tz < 8 {
			break
		}
	}
	bbuf |= 1 << tz // replace the marker bit
	return bbuf
}
