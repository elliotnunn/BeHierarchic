package sit

import (
	"io"
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

type BitReader struct { // copyable
	bg         ByteGetter // not at all constant
	bufs       [4][]byte  // enough for 4 bytes, enough for anyone...
	nextoffset int64
	bit        uint8 // number of bits to skip from this one [0,7]
	enderr     error
}

func NewBitReader(bg ByteGetter) BitReader {
	return BitReader{bg: bg}
}

func (b *BitReader) SacrificeBuffer() {
	clear(b.bufs[:]) // TODO no this is wrong
}

/*
fast-path is a single 4-byte big/little endian read, followed by shifting and masking



*/

// "MinBytes" is an interesting concept
// (we trust that each call to GetBytes returns 4+ bytes except in EOF)
// maybe this isn't the right way, maybe just a small chunk should be taken? yeah...
// func (b *BitReader) Next() error {
// 	if b.enderr != nil {
// 		return b.enderr
// 	}
// 	nu, err := b.bg.GetBytes(b.nextoffset)
// 	b.nextoffset += int64(len(nu))
// 	b.enderr = err
// 	if len(nu) == 0 {
// 		return b.enderr
// 	} else if len(b.bufs[0]) == 0 {
// 		b.bufs[0] = nu
// 	} else {
// 		b.futurebufs = append(b.futurebufs, nu)
// 	}
// 	return nil
// }

// func (b *BitReader) next() ([]byte, error) {

// }

func (b *BitReader) ensurebyteavail() error {
	if len(b.bufs[0]) > 0 {
		return nil
	}
	if b.enderr != nil {
		return b.enderr
	}
	b.bufs[0], b.enderr = b.bg.GetBytes(b.nextoffset)
	b.nextoffset += int64(len(b.bufs[0]))
	if len(b.bufs[0]) > 0 {
		return nil
	}
	return b.enderr
}

func (b *BitReader) step1() {
	b.bufs[0] = b.bufs[0][1:]
	if len(b.bufs[0]) == 0 {
		copy(b.bufs[0:], b.bufs[1:])
		b.bufs[len(b.bufs)-1] = nil
	}
	b.bit -= 8
}

func (b *BitReader) next4() ([4]byte, error) {
	var ret [4]byte
	n := 0
	for i := range b.bufs {
		if len(b.bufs[i]) == 0 {
			b.bufs[i], b.enderr = b.bg.GetBytes(b.nextoffset)
		}
		n += copy(ret[n:], b.bufs[i])
		if n == 4 {
			return ret, nil
		}
	}
	return ret, b.enderr
}

func (b *BitReader) ReadHiBits(n int) (uint32, error) { // bigendian
	if len(b.bufs[0]) == 0 { // is new or has been SacrificeBuffer'd
		if b.bit == 0 {
			b.bit = 0x80
		}
		var err error
		b.bufs[0], err = b.bg.GetBytes(b.nextoffset)
		if len(b.bufs[0]) == 0 {
			return 0, err
		}
	}

	// might be worth optimising the below loop at some point in future
	var ret uint32
	for i := range n {
		ret <<= 1
		if b.bufs[0][0]&b.bit != 0 {
			ret |= 1
		}
		b.bit >>= 1
		if b.bit == 0 {
			b.bit = 0x80
			b.bufs[0] = b.bufs[0][1:]
			b.nextoffset++
			if i < n-1 && len(b.bufs[0]) == 0 { // will we need more bits soon?
				var err error
				b.bufs[0], err = b.bg.GetBytes(b.nextoffset)
				if len(b.bufs[0]) == 0 {
					return 0, err
				}
			}
		}
	}
	return ret, nil
}

func (b *BitReader) ReadLoBits(n uint8) (uint32, error) { // littleendian
	if len(b.bufs[0]) >= 4 { // yay fast path
		v := uint32(b.bufs[0][0]) | uint32(b.bufs[0][1])<<8 | uint32(b.bufs[0][2])<<16 | uint32(b.bufs[0][3])<<24
		v >>= b.bit
		v &= uint32(1)<<n - 1
		nb := b.bit + n
		b.bufs[0] = b.bufs[0][nb/8:]
		b.bit = nb % 8
		return v, nil
	} else { // sad slow path
		v := uint32(0)
		for i := range n {
			err := b.ensurebyteavail()
			if err != nil {
				return v, err
			}
			if b.bufs[0][0]&(1<<b.bit) != 0 {
				v |= uint32(1) << i
			}
			b.bit++
			if b.bit == 8 {
				b.step1()
			}
		}
		return v, nil
	}
}

// only works correctly if the bits were previously read
func (b *BitReader) DiscardBits(n uint8) {
	b.bit += n
	for b.bit >= 8 && len(b.bufs[0]) > 0 {
		b.step1()
	}
	if b.bit >= 8 {
		panic("discarding bits that were never read")
	}
}

// func (b *BitReader) ReadBool() (bool, error) {
// again:
// 	if len(b.bufs[0]) >= 0 { // yay fast path
// 		ret := b.bufs[0][0]>>b.bit&1 != 0
// 		b.bit++
// 		if b.bit == 8 {
// 			b.step1()
// 		}
// 		return ret, nil
// 	} else { // sad slow path
// 		err := b.ensurebyteavail()
// 		if err != nil {
// 			return false, err
// 		}
// 		goto again
// 	}
// }

func (b *BitReader) ReadLoBitsTemp(n uint8) (uint32, error) {
	if len(b.bufs[0]) >= 4 { // yay fast path
		v := uint32(b.bufs[0][0]) | uint32(b.bufs[0][1])<<8 | uint32(b.bufs[0][2])<<16 | uint32(b.bufs[0][3])<<24
		v >>= b.bit
		v &= uint32(1)<<n - 1
		return v, nil
	} else { // sad slow path
		buf, err := b.next4()
		v := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
		v >>= b.bit
		v &= uint32(1)<<n - 1
		return v, err
	}
}
