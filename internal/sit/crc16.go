package sit

import (
	"encoding/binary"
	"io"
)

var crctab [256]uint16

func init() {
	for i := range uint16(256) {
		k := i
		for range 8 {
			if k&1 != 0 {
				k = (k >> 1) ^ 0xa001
			} else {
				k >>= 1
			}
		}
		crctab[i] = k
	}
}

type crc16reader struct {
	r         io.ReadCloser
	len       int64
	want, got uint16
}

func (r *crc16reader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.update(p[:n])

	if r.len == 0 && r.got != r.want {
		err = ErrChecksum
	}
	return
}

func (r *crc16reader) update(buffer []byte) {
	check := r.got
	for _, ch := range buffer {
		check = crctab[byte(check)^ch] ^ check>>8
	}
	r.got = check
	r.len -= int64(len(buffer))
}

func (r *crc16reader) Close() error { return r.r.Close() }

func readStructCheckingCRC16(r io.ReaderAt, offset int64, structSize, crcField int) ([]byte, error) {
	if structSize < 0 { // is itself the offset of a field!
		structSizeField := -structSize
		var smallbuf [2]byte
		n, err := r.ReadAt(smallbuf[:], offset+int64(structSizeField))
		if n != 2 {
			return nil, err
		}
		structSize = int(binary.BigEndian.Uint16(smallbuf[:]))
	}

	buf := make([]byte, max(structSize, crcField+2))
	n, err := r.ReadAt(buf, offset)
	if n != len(buf) {
		return buf[:n], err
	}

	want := binary.BigEndian.Uint16(buf[crcField:])
	got := uint16(0)
	for i, ch := range buf[:structSize] {
		if i == crcField || i == crcField+1 {
			ch = 0
		}
		got = crctab[byte(got)^ch] ^ got>>8
	}
	if got != want {
		return nil, ErrChecksum
	}

	return buf[:structSize], err
}

func checkCRC16(buf []byte, crcField int) bool {
	want := binary.BigEndian.Uint16(buf[crcField:])
	got := uint16(0)
	for i, ch := range buf {
		if i == crcField || i == crcField+1 {
			ch = 0
		}
		got = crctab[byte(got)^ch] ^ got>>8
	}
	return got == want
}

func calcCRC16(buf []byte) uint16 {
	got := uint16(0)
	for _, ch := range buf {
		got = crctab[byte(got)^ch] ^ got>>8
	}
	return got
}
