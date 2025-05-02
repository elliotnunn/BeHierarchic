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

import "io"

func huffman(o io.ByteWriter, i bitreader) error {
	numfreetree := 0 /* number of free np.one nodes */

	/* 515 because StuffIt Classic needs more than the needed 511 */
	type node struct {
		one, zero int
		byte      uint8
	}
	nodelist := make([]node, 515)
	np, npb := 0, 0

	for { /* removed recursion, optimized a lot */
		for {
			np = npb
			npb++
			b, err := i.ReadBits(1)
			if err != nil {
				return err
			}
			if b == 1 {
				b, err := i.ReadBits(8)
				if err != nil {
					return err
				}
				nodelist[np].byte = byte(b)
				nodelist[np].zero, nodelist[np].one = -1, -1
				break
			} else {
				nodelist[np].zero = npb
				numfreetree++
			}
		}
		numfreetree--
		if numfreetree != -1 {
			for nodelist[np].one != -1 {
				np--
			}
			nodelist[np].one = npb
		}

		if numfreetree < 0 {
			break
		}
	}

	for {
		b, err := i.ReadBits(1)
		if err != nil {
			return err
		}
		np = 0
		for nodelist[np].one != -1 {
			if b == 1 {
				np = nodelist[np].one
			} else {
				np = nodelist[np].zero
			}
		}
		err = o.WriteByte(nodelist[np].byte)
		if err != nil {
			return err
		}
	}
}
