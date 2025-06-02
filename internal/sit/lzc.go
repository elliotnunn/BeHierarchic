package sit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

func lzc(r io.Reader, dstsize uint32) io.ReadCloser { // should it be possible to return an error?
	pr, pw := io.Pipe()
	go lzccopy(pw, r, dstsize)
	return pr
}

func lzccopy(dst *io.PipeWriter, src io.Reader, dstsize uint32) {
	br := bufio.NewReaderSize(src, 1024)
	bw := bufio.NewWriterSize(dst, 1024)
	defer bw.Flush()

	var stack []byte
	const maxbits = 14
	const blockcomp = true
	const maxmaxcode = 1 << maxbits
	nbits := 9
	maxcode := 1<<nbits - 1
	free_ent := int32(257)
	clearflag := false

	prefixtab := make([]uint16, maxmaxcode)
	suffixtab := make([]byte, maxmaxcode)

	for i := range 256 {
		suffixtab[i] = byte(i)
	}

	fmt.Println("hi!")

	bitbuf := InitialLittleEndian

	getcode := func() int32 {
		fmt.Printf("precode maxcode %d free_ent %d\n", maxcode, free_ent)
		if free_ent > int32(maxcode) {
			nbits++
			if nbits == maxbits {
				maxcode = maxmaxcode
			} else {
				maxcode = 1<<nbits - 1
			}
		}
		if clearflag {
			nbits = 9
			maxcode = 1<<nbits - 1
			clearflag = false
		}

		bitbuf = FillLittleEndian(bitbuf, br)
		code := int32(bitbuf & (1<<nbits - 1))
		bitbuf >>= nbits
		fmt.Printf("code %d width %d\n", code, nbits)
		return code
	}

	bitbuf = FillLittleEndian(bitbuf, br)
	oldcode := getcode()
	finchar := byte(oldcode)
	bw.WriteByte(finchar)
	fmt.Printf("writebyte %02x\n", finchar)
	dstsize--

	for {
		code := getcode()

		if code == 256 {
			clear(prefixtab[:256])
			// clear_flg stuff ignored
			clearflag = true
			free_ent = 256
			code = getcode()
			if code == -1 {
				break
			}
		}
		incode := code

		if code >= free_ent {
			if code > free_ent {
				bw.Flush()
				dst.CloseWithError(errors.New("illegal data"))
				return
			}
			stack = append(stack, finchar)
			code = oldcode
		}

		for code >= 256 {
			stack = append(stack, suffixtab[code])
			code = int32(prefixtab[code])
		}
		finchar = suffixtab[code]
		stack = append(stack, finchar)

		for i := len(stack) - 1; i >= 0; i-- {
			bw.WriteByte(stack[i])
			fmt.Printf("writebyte %02x\n", stack[i])
			dstsize--
			if dstsize == 0 {
				bw.Flush()
				dst.Close()
				return
			}
		}
		stack = stack[:0]

		code = free_ent
		if code < maxmaxcode {
			prefixtab[code] = uint16(oldcode)
			suffixtab[code] = finchar
			free_ent = code + 1
		}
		oldcode = incode
	}
}
