package sit

import "io"

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
	bg      ByteGetter // not at all constant
	current []byte
	curbase int64
	bit     uint8 // mask of the next bit to read
}

func NewBitReader(bg ByteGetter) BitReader {
	return BitReader{bg: bg}
}

func (b *BitReader) SacrificeBuffer() {
	b.current = nil
}

func (b *BitReader) ReadHiBits(n int) (uint32, error) {
	if len(b.current) == 0 { // is new or has been SacrificeBuffer'd
		if b.bit == 0 {
			b.bit = 0x80
		}
		var err error
		b.current, err = b.bg.GetBytes(b.curbase)
		if len(b.current) == 0 {
			return 0, err
		}
	}

	// might be worth optimising the below loop at some point in future
	var ret uint32
	for i := range n {
		ret <<= 1
		if b.current[0]&b.bit != 0 {
			ret |= 1
		}
		b.bit >>= 1
		if b.bit == 0 {
			b.bit = 0x80
			b.current = b.current[1:]
			b.curbase++
			if i < n-1 && len(b.current) == 0 { // will we need more bits soon?
				var err error
				b.current, err = b.bg.GetBytes(b.curbase)
				if len(b.current) == 0 {
					return 0, err
				}
			}
		}
	}
	return ret, nil
}
