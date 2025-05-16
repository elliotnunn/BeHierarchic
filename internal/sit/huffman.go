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
	"fmt"
	"io"

	"github.com/elliotnunn/resourceform/internal/decompressioncache"
)

type node struct {
	one, zero int
	byte      uint8
}

// Okay, the mission is to convert this into a "stepper" function
// ... with abstractions like BitReader, which is a bit of a tricky thing...

func printHuffmanTable(nodelist []node) {
	for i, node := range nodelist {
		fmt.Printf("NODE % 2d: (0)=(%d) (1)=(%d) (byte)=%02x\n",
			i, node.zero, node.one, node.byte)
	}
}

func InitHuffman(r io.ReaderAt, size int64) decompressioncache.Stepper { // should it be possible to return an error?
	byteGetter := NewByteGetter(r)
	bitReader := NewBitReader(byteGetter)

	var nodelist []node
	numfreetree := 0
	for { /* removed recursion, optimized a lot */
		var np int
		for {
			np = len(nodelist)
			nodelist = append(nodelist, node{})
			bit, err := bitReader.ReadBits(1)
			if err != nil {
				panic(err)
			}
			if bit == 1 {
				byt, err := bitReader.ReadBits(8)
				if err != nil {
					panic(err)
				}
				nodelist[np].byte = byte(byt)
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

	bitReader.SacrificeBuffer()
	return func() (decompressioncache.Stepper, []byte, error) {
		return stepHuffman(bitReader, nodelist, size)
	}
}

func stepHuffman(br BitReader, huff []node, remain int64) (decompressioncache.Stepper, []byte, error) {
	accum := make([]byte, 0, min(4096, int(remain)))
	for range cap(accum) {
		node := 0
		for huff[node].one != -1 {
			which, err := br.ReadBits(1)
			if err != nil {
				return nil, accum, err
			}
			if which == 0 {
				node = huff[node].zero
			} else {
				node = huff[node].one
			}
		}
		accum = append(accum, huff[node].byte)
	}

	br.SacrificeBuffer()
	return func() (decompressioncache.Stepper, []byte, error) {
		return stepHuffman(br, huff, remain-int64(len(accum)))
	}, accum, nil
}
