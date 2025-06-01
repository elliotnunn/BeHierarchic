/*
StuffIt file archiver client

XAD library system for archive handling
Copyright (C) 1998 and later by Dirk Stoecker <soft@dstoecker.de>

little based on macutils 2.0b3 macunpack by Dik T. Winter
Copyright (C) 1992 Dik T. Winter <dik@cwi.nl>

algorithm 15 is based on the work of  Matthew T. Russotto
Copyright (C) 2002 Matthew T. Russotto <russotto@speakeasy.net>
http://www.speakeasy.org/~russotto/arseniccomp.html

ported to Go
Copyright (C) 2025 Elliot Nunn

This library is free software; you can redistribute it and/or
modify it under the terms of the GNU Lesser General Public
License as published by the Free Software Foundation; either
version 2.1 of the License, or (at your option) any later version.

This library is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
Lesser General Public License for more details.

You should have received a copy of the GNU Lesser General Public
License along with this library; if not, write to the Free Software
Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA  02111-1307  USA
*/

package sit

import (
	"bufio"
	"fmt"
	"io"
	"math/bits"
)

type node struct {
	one, zero int
	byte      uint8
}

func huffman(r io.Reader, dstsize uint32) io.ReadCloser { // should it be possible to return an error?
	pr, pw := io.Pipe()
	go huffcopy(pw, r, dstsize)
	return pr
}

func huffcopy(dst io.WriteCloser, src io.Reader, dstsize uint32) {
	defer dst.Close()
	br := bufio.NewReaderSize(src, 1024)
	bw := bufio.NewWriterSize(dst, 1024)
	defer bw.Flush()

	bitbuf := InitialBigEndian
	var nodelist []node
	numfreetree := 0
	for { /* removed recursion, optimized a lot */
		var np int
		for {
			np = len(nodelist)
			nodelist = append(nodelist, node{})
			bitbuf = FillBigEndian(bitbuf, br)
			bit := bitbuf&(1<<(bits.UintSize-1)) != 0
			bitbuf <<= 1
			if bit {
				nodelist[np].byte = byte(bitbuf >> (bits.UintSize - 8))
				// fmt.Printf("byte %02x\n", nodelist[np].byte)
				bitbuf <<= 8
				nodelist[np].zero, nodelist[np].one = -1, -1
				break
			} else {
				nodelist[np].zero = len(nodelist)
				numfreetree++
			}
		}
		numfreetree--
		if numfreetree != -1 {
			for nodelist[np].one != 0 {
				np--
			}
			nodelist[np].one = len(nodelist)
		}
		if numfreetree < 0 {
			break
		}
	}

	for range dstsize {
		node := 0
		bitbuf = FillBigEndian(bitbuf, br) // guaranteed to be enough bits for a whole huff code
		for nodelist[node].one != -1 {
			bit := bitbuf&(1<<(bits.UintSize-1)) != 0
			bitbuf <<= 1
			if !bit {
				node = nodelist[node].zero
			} else {
				node = nodelist[node].one
			}
		}
		err := bw.WriteByte(nodelist[node].byte)
		if err != nil {
			return // nowhere to report this
		}
	}

}

func printHuffmanTable(nodelist []node) {
	for i, node := range nodelist {
		fmt.Printf("NODE % 2d: (0)=(%d) (1)=(%d) (byte)=%02x\n",
			i, node.zero, node.one, node.byte)
	}
}
