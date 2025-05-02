package sit

import "io"

type bitreader interface {
	io.ByteReader
	ReadBits(n int) (uint, error)
}
