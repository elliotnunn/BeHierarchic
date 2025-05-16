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
	"errors"
	"fmt"
	"io"

	"github.com/elliotnunn/resourceform/internal/decompressioncache"
)

const (
	half = 1 << 24
)

var SIT_rndtable = []uint16{
	0xee, 0x56, 0xf8, 0xc3, 0x9d, 0x9f, 0xae, 0x2c,
	0xad, 0xcd, 0x24, 0x9d, 0xa6, 0x101, 0x18, 0xb9,
	0xa1, 0x82, 0x75, 0xe9, 0x9f, 0x55, 0x66, 0x6a,
	0x86, 0x71, 0xdc, 0x84, 0x56, 0x96, 0x56, 0xa1,
	0x84, 0x78, 0xb7, 0x32, 0x6a, 0x3, 0xe3, 0x2,
	0x11, 0x101, 0x8, 0x44, 0x83, 0x100, 0x43, 0xe3,
	0x1c, 0xf0, 0x86, 0x6a, 0x6b, 0xf, 0x3, 0x2d,
	0x86, 0x17, 0x7b, 0x10, 0xf6, 0x80, 0x78, 0x7a,
	0xa1, 0xe1, 0xef, 0x8c, 0xf6, 0x87, 0x4b, 0xa7,
	0xe2, 0x77, 0xfa, 0xb8, 0x81, 0xee, 0x77, 0xc0,
	0x9d, 0x29, 0x20, 0x27, 0x71, 0x12, 0xe0, 0x6b,
	0xd1, 0x7c, 0xa, 0x89, 0x7d, 0x87, 0xc4, 0x101,
	0xc1, 0x31, 0xaf, 0x38, 0x3, 0x68, 0x1b, 0x76,
	0x79, 0x3f, 0xdb, 0xc7, 0x1b, 0x36, 0x7b, 0xe2,
	0x63, 0x81, 0xee, 0xc, 0x63, 0x8b, 0x78, 0x38,
	0x97, 0x9b, 0xd7, 0x8f, 0xdd, 0xf2, 0xa3, 0x77,
	0x8c, 0xc3, 0x39, 0x20, 0xb3, 0x12, 0x11, 0xe,
	0x17, 0x42, 0x80, 0x2c, 0xc4, 0x92, 0x59, 0xc8,
	0xdb, 0x40, 0x76, 0x64, 0xb4, 0x55, 0x1a, 0x9e,
	0xfe, 0x5f, 0x6, 0x3c, 0x41, 0xef, 0xd4, 0xaa,
	0x98, 0x29, 0xcd, 0x1f, 0x2, 0xa8, 0x87, 0xd2,
	0xa0, 0x93, 0x98, 0xef, 0xc, 0x43, 0xed, 0x9d,
	0xc2, 0xeb, 0x81, 0xe9, 0x64, 0x23, 0x68, 0x1e,
	0x25, 0x57, 0xde, 0x9a, 0xcf, 0x7f, 0xe5, 0xba,
	0x41, 0xea, 0xea, 0x36, 0x1a, 0x28, 0x79, 0x20,
	0x5e, 0x18, 0x4e, 0x7c, 0x8e, 0x58, 0x7a, 0xef,
	0x91, 0x2, 0x93, 0xbb, 0x56, 0xa1, 0x49, 0x1b,
	0x79, 0x92, 0xf3, 0x58, 0x4f, 0x52, 0x9c, 0x2,
	0x77, 0xaf, 0x2a, 0x8f, 0x49, 0xd0, 0x99, 0x4d,
	0x98, 0x101, 0x60, 0x93, 0x100, 0x75, 0x31, 0xce,
	0x49, 0x20, 0x56, 0x57, 0xe2, 0xf5, 0x26, 0x2b,
	0x8a, 0xbf, 0xde, 0xd0, 0x83, 0x34, 0xf4, 0x17,
}

type arsenicData struct {
	br BitReader

	Range uint32
	Code  uint32

	/* SIT_dounmntf function private */
	inited int32 /* init 0 */
	moveme [256]uint8

	blockbits   uint16
	frequencies [nfreq]uint32
}

type arsenicModel struct {
	increment int32
	maxfreq   int32
	entries   int32
	tabloc    [256]uint32
	offset    uint16
	syms      []uint16
}

var arsenicModels = struct {
	initial_model arsenicModel
	selmodel      arsenicModel
	mtfmodel      [7]arsenicModel
}{}

const nfreq = 276

func init() {
	n := uint16(0)
	n += initArsenicModel(&arsenicModels.initial_model, n, 2, 0, 1, 256)
	n += initArsenicModel(&arsenicModels.selmodel, n, 11, 0, 8, 1024)
	/* selector model: 11 selections, starting at 0, 8 increment, 1024 maxfreq */

	n += initArsenicModel(&arsenicModels.mtfmodel[0], n, 2, 2, 8, 1024)
	/* model 3: 2 symbols, starting at 2, 8 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[1], n, 4, 4, 4, 1024)
	/* model 4: 4 symbols, starting at 4, 4 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[2], n, 8, 8, 4, 1024)
	/* model 5: 8 symbols, starting at 8, 4 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[3], n, 0x10, 0x10, 4, 1024)
	/* model 6: $10 symbols, starting at $10, 4 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[4], n, 0x20, 0x20, 2, 1024)
	/* model 7: $20 symbols, starting at $20, 2 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[5], n, 0x40, 0x40, 2, 1024)
	/* model 8: $40 symbols, starting at $40, 2 increment, 1024 maxfreq */
	n += initArsenicModel(&arsenicModels.mtfmodel[6], n, 0x80, 0x80, 1, 1024)
	/* model 9: $80 symbols, starting at $80, 1 increment, 1024 maxfreq */
	if n != nfreq {
		panic("wrong nfreq")
	}
}

func (sa *arsenicData) model2String(s *arsenicModel) string {
	frequencies := sa.frequencies[s.offset:]
	ret := []byte(fmt.Sprintf("MODEL inc=%d maxfreq=%d tabloc=", s.increment, s.maxfreq))
	for _, t := range s.tabloc {
		ret = append(ret, fmt.Sprintf("%d/", t)...)
	}
	for string(ret[len(ret)-3:]) == "/0/" {
		ret = ret[:len(ret)-2]
	}
	ret[len(ret)-1] = '\n'
	for i := range min(s.entries, int32(len(s.syms))) {
		ret = append(ret, fmt.Sprintf("  %d=(s=%d/f=%d)\n", i, s.syms[i], frequencies[i])...)
	}
	return string(ret[:len(ret)-1])
}

func (sa *arsenicData) updateModel(mymod *arsenicModel, symindex int32) {
	frequencies := sa.frequencies[mymod.offset:]
	for i := range symindex {
		frequencies[i] += uint32(mymod.increment)
	}
	if frequencies[0] > uint32(mymod.maxfreq) {
		for i := range mymod.entries {
			/* no -1, want to include the 0 entry */
			/* this converts cumfreqs LONGo frequencies, then shifts right */
			frequencies[i] -= frequencies[i+1]
			frequencies[i]++ /* avoid losing things entirely */
			frequencies[i] >>= 1
		}
		/* then convert frequencies back to cumfreq */
		for i := mymod.entries - 1; i >= 0; i-- {
			frequencies[i] += frequencies[i+1]
		}
	}
}

func (sa *arsenicData) getCode(symhigh uint32, symlow uint32, symtot uint32) { /* aka remove symbol */
	renorm_factor := sa.Range / symtot
	lowincr := renorm_factor * symlow
	sa.Code -= lowincr
	if symhigh == symtot {
		sa.Range -= lowincr
	} else {
		sa.Range = (symhigh - symlow) * renorm_factor
	}

	nbits := 0
	for sa.Range <= half {
		sa.Range <<= 1
		sa.Code <<= 1
		nbits++
	}
	b, _ := sa.br.ReadHiBits(nbits)
	sa.Code |= b
}

func (sa *arsenicData) getSym(model *arsenicModel) int32 {
	frequencies := sa.frequencies[model.offset:]

	/* getfreq */
	freq := int32(sa.Code / (sa.Range / frequencies[0]))
	var i int32
	for i = 1; i < model.entries; i++ {
		if frequencies[i] <= uint32(freq) {
			break
		}
	}
	sym := int32(model.syms[i-1])
	sa.getCode(frequencies[i-1], frequencies[i], frequencies[0])
	sa.updateModel(model, i)
	return sym
}

func (sa *arsenicData) reinitModel(mymod *arsenicModel) {
	cumfreq := mymod.entries * mymod.increment

	frequencies := sa.frequencies[mymod.offset:]

	for i := range mymod.entries + 1 {
		/* <= sets last frequency to 0; there isn't really a symbol for that
		   last one  */
		frequencies[i] = uint32(cumfreq)
		cumfreq -= mymod.increment
	}
}

func initArsenicModel(newmod *arsenicModel, base uint16, entries int32, start int32, increment int32, maxfreq int32) uint16 {
	newmod.syms = make([]uint16, entries+1)
	newmod.increment = increment
	newmod.maxfreq = maxfreq
	newmod.entries = entries
	newmod.offset = base
	for i := range entries {
		newmod.tabloc[(entries-i-1)+start] = uint32(i)
		newmod.syms[i] = uint16((entries - i - 1) + start)
	}
	return uint16(entries + 1)
}

func (sa *arsenicData) arithGetBits(model *arsenicModel, nbits int32) uint32 {
	/* the model is assumed to be a binary one */
	addme := uint32(1)
	accum := uint32(0)
	for range nbits {
		if sa.getSym(model) != 0 {
			accum += addme
		}
		addme += addme
	}
	return accum
}

func (sa *arsenicData) doUnmtf(sym int32) int32 {
	if sym == -1 || sa.inited == 0 {
		for i := range 256 {
			sa.moveme[i] = uint8(i)
		}
		sa.inited = 1
	}
	if sym == -1 {
		return 0
	}
	result := int32(sa.moveme[sym])
	for i := sym; i > 0; i-- {
		sa.moveme[i] = sa.moveme[i-1]
	}

	sa.moveme[0] = uint8(result)
	return result
}

func (sa *arsenicData) unblockSort(block []uint8, blocklen uint32, last_index uint32, outblock []uint8) {
	var counts [256]uint32
	var cumcounts [256]uint32
	xform := make([]uint32, blocklen)

	for _, b := range block {
		counts[b]++
	}

	cum := uint32(0)
	for i := range 256 {
		cumcounts[i] = cum
		cum += counts[i]
		counts[i] = 0
	}

	for i, b := range block {
		xform[cumcounts[b]+counts[b]] = uint32(i)
		counts[b]++
	}

	j := xform[last_index]
	for i := range block {
		outblock[i] = block[j]
		//      block[j] = 0xa5; /* for debugging */
		j = xform[j]
	}
}

func writeAndUnrleAndUnrnd(accum []byte, block []byte, rnd int16) []byte {
	count := 0
	last := uint8(0)

	rndindex := 0
	rndcount := SIT_rndtable[rndindex]
	for _, ch := range block {
		if rnd != 0 && (rndcount == 0) {
			ch ^= 1
			rndindex++
			if rndindex == len(SIT_rndtable) {
				rndindex = 0
			}
			rndcount = SIT_rndtable[rndindex]
		}
		rndcount--

		if count == 4 {
			for range ch {
				accum = append(accum, last)
			}
			count = 0
		} else {
			accum = append(accum, ch)
			if ch != last {
				count = 0
				last = ch
			}
			count++
		}
	}
	return accum
}

func InitArsenic(r io.ReaderAt, size int64) decompressioncache.Stepper { // should it be possible to return an error?
	sa := arsenicData{
		br: NewBitReader(NewByteGetter(r)),
	}
	return func() (decompressioncache.Stepper, []byte, error) {
		return setupArsenic(sa, size)
	}
}

func setupArsenic(sa arsenicData, size int64) (rs decompressioncache.Stepper, rb []byte, re error) {
	defer func() {
		if recover() != nil {
			rs, rb, re = nil, nil, errors.New("internal panic")
		}
	}()

	sa.Range = 1 << 25
	sa.Code, _ = sa.br.ReadHiBits(26)

	sa.reinitModel(&arsenicModels.initial_model)
	if sa.arithGetBits(&arsenicModels.initial_model, 8) != 0x41 ||
		sa.arithGetBits(&arsenicModels.initial_model, 8) != 0x73 {
		return nil, nil, errors.New("arsenic data not starting with 'As'")
	}
	sa.blockbits = uint16(sa.arithGetBits(&arsenicModels.initial_model, 4) + 9)

	eob := sa.getSym(&arsenicModels.initial_model)
	if eob != 0 {
		return nil, nil, io.EOF
	}

	return stepArsenic(sa, size)
}

func stepArsenic(sa arsenicData, size int64) (rs decompressioncache.Stepper, rb []byte, re error) {
	defer func() {
		if recover() != nil {
			rs, rb, re = nil, nil, errors.New("internal panic")
		}
	}()

	sa.reinitModel(&arsenicModels.selmodel)
	for i := range 7 {
		sa.reinitModel(&arsenicModels.mtfmodel[i])
	}

	block := make([]byte, 0, 1<<sa.blockbits)
	unsortedblock := make([]byte, 1<<sa.blockbits)
	var accum []byte

	rnd := sa.getSym(&arsenicModels.initial_model)
	primary_index := int32(sa.arithGetBits(&arsenicModels.initial_model, int32(sa.blockbits)))
	stopme, repeatstate, repeatcount := 0, 0, 0
	for stopme == 0 {
		sel := sa.getSym(&arsenicModels.selmodel)
		sym := int32(0)
		switch sel {
		case 0:
			sym = -1
			if repeatstate == 0 {
				repeatstate, repeatcount = 1, 1
			} else {
				repeatstate += repeatstate
				repeatcount += repeatstate
			}
		case 1:
			if repeatstate == 0 {
				repeatstate = 1
				repeatcount = 2
			} else {
				repeatstate += repeatstate
				repeatcount += repeatstate
				repeatcount += repeatstate
			}
			sym = -1
		case 2:
			sym = 1
		case 10:
			stopme = 1
			sym = 0
		default:
			if (sel > 9) || (sel < 3) { /* this basically can't happen */
				panic("illegal selector")
			} else {
				sym = sa.getSym(&arsenicModels.mtfmodel[sel-3])
			}
		}

		if repeatstate != 0 && (sym >= 0) {
			repeatstate = 0
			setto := sa.doUnmtf(0)
			for range repeatcount {
				block = append(block, uint8(setto))
			}
			repeatcount = 0
		}
		if stopme == 0 && repeatstate == 0 {
			block = append(block, byte(sa.doUnmtf(sym)))
		}
	}
	sa.unblockSort(block, uint32(len(block)), uint32(primary_index), unsortedblock)
	accum = writeAndUnrleAndUnrnd(accum, unsortedblock, int16(rnd))
	eob := sa.getSym(&arsenicModels.initial_model)
	if int64(len(accum)) >= size || eob != 0 {
		return nil, accum[:min(size, int64(len(accum)))], io.EOF
	}
	sa.doUnmtf(-1)
	// there was a checksum here that we don't calculate

	sa.br.SacrificeBuffer()
	return func() (decompressioncache.Stepper, []byte, error) {
		return stepArsenic(sa, size-int64(len(accum)))
	}, accum, nil
}
