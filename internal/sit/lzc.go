// UNIX Compress

// XAD library system for archive handling
// Copyright (C) 1998 and later by Dirk Stoecker <soft@dstoecker.de>

// ported to Go
// Copyright (C) 2025 Elliot Nunn

// This library is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 2.1 of the License, or (at your option) any later version.

// This library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.

// You should have received a copy of the GNU Lesser General Public
// License along with this library; if not, write to the Free Software
// Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA  02111-1307  USA

package sit

import (
	"bufio"
	"errors"
	"io"
)

func lzc(r io.Reader, dstsize uint32) io.ReadCloser {
	pr, pw := io.Pipe()
	go lzccopy(pw, r, dstsize)
	return pr
}

func lzccopy(dst *io.PipeWriter, src io.Reader, dstsize uint32) {
	var reterr error
	br := bufio.NewReaderSize(src, 4096)
	bw := bufio.NewWriterSize(dst, 4096)
	defer func() {
		bw.Flush()
		dst.CloseWithError(reterr)
	}()

	var stack []byte
	const maxbits = 14
	const maxmaxcode = 1 << maxbits
	nbits := 9
	maxcode := uint16(1<<nbits - 1)
	free_ent := uint16(257)
	clearflag := false

	prefixtab := make([]uint16, maxmaxcode)
	suffixtab := make([]byte, maxmaxcode)

	for i := range 256 {
		suffixtab[i] = byte(i)
	}

	var buffer [16]byte // enough room to use LE loader instructions
	boffset, bsize := 0, 0

	getcode := func() (uint16, bool) {
		needNewBuf := boffset >= bsize
		if free_ent > maxcode {
			nbits++
			if nbits == maxbits {
				maxcode = maxmaxcode
			} else {
				maxcode = 1<<nbits - 1
			}
			needNewBuf = true
		}
		if clearflag {
			nbits = 9
			maxcode = 1<<nbits - 1
			clearflag = false
			needNewBuf = true
		}

		if needNewBuf {
			n, err := io.ReadFull(br, buffer[:nbits])
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			reterr = err
			if n == 0 {
				return 0, false
			}
			boffset = 0
			bsize = n*8 - (nbits - 1) // ensure no over-reading
		}

		byteoffset := boffset / 8
		bitoffset := boffset % 8
		code := ((uint32(buffer[byteoffset]) |
			uint32(buffer[byteoffset+1])<<8 |
			uint32(buffer[byteoffset+2])<<16) >> bitoffset) & (1<<nbits - 1)
		boffset += nbits
		return uint16(code), true
	}

	oldcode, ok := getcode()
	if !ok {
		return
	}
	finchar := byte(oldcode)
	if err := bw.WriteByte(finchar); err != nil {
		return
	}
	dstsize--
	if dstsize == 0 {
		return
	}

	for {
		code, ok := getcode()
		if !ok {
			return
		}

		if code == 256 {
			clear(prefixtab[:256])
			// clear_flg stuff ignored
			clearflag = true
			free_ent = 256
			code, ok = getcode()
			if !ok {
				return
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
			code = prefixtab[code]
		}
		finchar = suffixtab[code]
		stack = append(stack, finchar)

		for i := len(stack) - 1; i >= 0; i-- {
			if err := bw.WriteByte(stack[i]); err != nil {
				return
			}
			dstsize--
			if dstsize == 0 {
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
