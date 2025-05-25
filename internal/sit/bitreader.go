package sit

import (
	"io"
	"math/bits"
)

type ByteGetter interface {
	GetBytes(offset int64) ([]byte, error)
}

type MyByteGetter struct {
	reader io.ReaderAt
}

func NewByteGetter(from io.ReaderAt) ByteGetter {
	if already, ok := from.(ByteGetter); ok {
		return already
	} else {
		return MyByteGetter{reader: from}
	}
}

func (b MyByteGetter) GetBytes(offset int64) ([]byte, error) {
	ret := make([]byte, 4096)
	n, err := b.reader.ReadAt(ret, offset)
	ret = ret[:n]
	return ret, err
}

const (
	InitialLittleEndian int  = 1
	InitialBigEndian    uint = 1 << (bits.UintSize - 1)
)

type BitReader struct { // copyable
	buf        []byte
	nextoffset int64
	Error      error
	bg         ByteGetter // not at all constant
}

func NewBitReader(bg ByteGetter) BitReader {
	return BitReader{bg: bg}
}

func (b *BitReader) SacrificeBuffer() {
	b.nextoffset -= int64(len(b.buf))
	b.buf = nil
}

func (b *BitReader) FillLittleEndian(bbuf int) int {
	// leadingzeros + 1 + goodbits = UintSize
	// unless leadingzeros = 0, in which case we don't know goodbits
	lz := bits.LeadingZeros(uint(bbuf))
	if lz <= 9 {
		return bbuf
	}

	goodbits := bits.UintSize - lz - 1
	bbuf &= ^(1 << goodbits) // clear the marker bit
	for {
		if len(b.buf) == 0 {
			if b.Error != nil {
				return int(uint(bbuf) | 1<<(bits.UintSize-1)) // poison the top bit
			}
			b.buf, b.Error = b.bg.GetBytes(b.nextoffset)
			b.nextoffset += int64(len(b.buf))
		}
		bbuf |= int(b.buf[0]) << goodbits
		b.buf = b.buf[1:]
		goodbits += 8
		if goodbits+10 > bits.UintSize {
			break
		}
	}
	bbuf |= 1 << goodbits // replace the marker bit
	return bbuf
}

// This one is slightly trickier because we can't use the right-shift-sign-bit trick
func (b *BitReader) FillBigEndian(bbuf uint) uint {
	// goodbits + 1 + trailingzeros = UintSize
	// unless leadingzeros = 0, in which case we don't know goodbits
	tz := bits.TrailingZeros(bbuf)
	if tz < 8 || tz == bits.UintSize {
		return bbuf
	}

	bbuf &= ^(1 << tz) // clear the marker bit
	for {
		if len(b.buf) == 0 {
			if b.Error != nil {
				break
			}
			b.buf, b.Error = b.bg.GetBytes(b.nextoffset)
			b.nextoffset += int64(len(b.buf))
		}
		tz -= 8
		bbuf |= uint(b.buf[0]) << (tz + 1)
		b.buf = b.buf[1:]
		if tz < 8 {
			break
		}
	}
	bbuf |= 1 << tz // replace the marker bit
	return bbuf
}
