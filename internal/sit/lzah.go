// StuffIt file archiver client

// XAD library system for archive handling
// Copyright (C) 1998 and later by Dirk Stoecker <soft@dstoecker.de>

// little based on macutils 2.0b3 macunpack by Dik T. Winter
// Copyright (C) 1992 Dik T. Winter <dik@cwi.nl>

// algorithm 15 is based on the work of  Matthew T. Russotto
// Copyright (C) 2002 Matthew T. Russotto <russotto@speakeasy.net>
// http://www.speakeasy.org/~russotto/arseniccomp.html

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

// const (
// 	SIT_VERSION        = 1
// 	SIT_REVISION       = 12
// 	SIT5_VERSION       = SIT_VERSION
// 	SIT5_REVISION      = SIT_REVISION
// 	SIT5EXE_VERSION    = SIT_VERSION
// 	SIT5EXE_REVISION   = SIT_REVISION
// 	MACBINARY_VERSION  = SIT_VERSION
// 	MACBINARY_REVISION = SIT_REVISION
// 	PACKIT_VERSION     = SIT_VERSION
// 	PACKIT_REVISION    = SIT_REVISION

// 	SITFH_COMPRMETHOD  = 0   /* uint8 rsrc fork compression method */
// 	SITFH_COMPDMETHOD  = 1   /* uint8 data fork compression method */
// 	SITFH_FNAMESIZE    = 2   /* uint8 filename size */
// 	SITFH_FNAME        = 3   /* uint8 83 byte filename */
// 	SITFH_FTYPE        = 66  /* uint32 file type */
// 	SITFH_CREATOR      = 70  /* uint32 file creator */
// 	SITFH_FNDRFLAGS    = 74  /* uint16 Finder flags */
// 	SITFH_CREATIONDATE = 76  /* uint32 creation date */
// 	SITFH_MODDATE      = 80  /* uint32 modification date */
// 	SITFH_RSRCLENGTH   = 84  /* uint32 decompressed rsrc length */
// 	SITFH_DATALENGTH   = 88  /* uint32 decompressed data length */
// 	SITFH_COMPRLENGTH  = 92  /* uint32 compressed rsrc length */
// 	SITFH_COMPDLENGTH  = 96  /* uint32 compressed data length */
// 	SITFH_RSRCCRC      = 100 /* uint16 crc of rsrc fork */
// 	SITFH_DATACRC      = 102 /* uint16 crc of data fork */ /* 6 reserved bytes */
// 	SITFH_HDRCRC       = 110 /* uint16 crc of file header */
// 	SIT_FILEHDRSIZE    = 112

// 	SITAH_SIGNATURE  = 0  /* uint32 signature = 'SIT!' */
// 	SITAH_NUMFILES   = 4  /* uint16 number of files in archive */
// 	SITAH_ARCLENGTH  = 6  /* uint32 arcLength length of entire archive incl. header */
// 	SITAH_SIGNATURE2 = 10 /* uint32 signature2 = 'rLau' */
// 	SITAH_VERSION    = 14 /* uint8 version number */
// 	SIT_ARCHDRSIZE   = 22 /* +7 reserved bytes */

// 	/* compression methods */
// 	SITnocomp  = 0 /* just read each byte and write it to archive */
// 	SITrle     = 1 /* RLE compression */
// 	SITlzc     = 2 /* LZC compression */
// 	SIThuffman = 3 /* Huffman compression */

// 	SITlzah   = 5 /* LZ with adaptive Huffman */
// 	SITfixhuf = 6 /* Fixed Huffman table */

// 	SITmw = 8 /* Miller-Wegman encoding */

// 	SITprot    = 16 /* password protected bit */
// 	SITsfolder = 32 /* start of folder */
// 	SITefolder = 33 /* end of folder */
// )

// type SITPrivate struct {
// CRC uint16
// Method uint8
// };

// const SITESC =  0x90    /* repeat packing escape */

// /* Note: compare with LZSS decoding in lharc! */
// const SITLZAH_N =       314
// const SITLZAH_T =       (2*SITLZAH_N-1)
// /*      Huffman table used for first 6 bits of offset:
//         #bits   codes
//         3       0x000
//         4       0x040-0x080
//         5       0x100-0x2c0
//         6       0x300-0x5c0
//         7       0x600-0xbc0
//         8       0xc00-0xfc0
// */

// var  SITLZAH_HuffCode []uint8 = {
//   0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
//   0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
//   0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
//   0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
//   0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04,
//   0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04,
//   0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08,
//   0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08,
//   0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c,
//   0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c, 0x0c,
//   0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
//   0x14, 0x14, 0x14, 0x14, 0x14, 0x14, 0x14, 0x14,
//   0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18,
//   0x1c, 0x1c, 0x1c, 0x1c, 0x1c, 0x1c, 0x1c, 0x1c,
//   0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
//   0x24, 0x24, 0x24, 0x24, 0x24, 0x24, 0x24, 0x24,
//   0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28,
//   0x2c, 0x2c, 0x2c, 0x2c, 0x2c, 0x2c, 0x2c, 0x2c,
//   0x30, 0x30, 0x30, 0x30, 0x34, 0x34, 0x34, 0x34,
//   0x38, 0x38, 0x38, 0x38, 0x3c, 0x3c, 0x3c, 0x3c,
//   0x40, 0x40, 0x40, 0x40, 0x44, 0x44, 0x44, 0x44,
//   0x48, 0x48, 0x48, 0x48, 0x4c, 0x4c, 0x4c, 0x4c,
//   0x50, 0x50, 0x50, 0x50, 0x54, 0x54, 0x54, 0x54,
//   0x58, 0x58, 0x58, 0x58, 0x5c, 0x5c, 0x5c, 0x5c,
//   0x60, 0x60, 0x64, 0x64, 0x68, 0x68, 0x6c, 0x6c,
//   0x70, 0x70, 0x74, 0x74, 0x78, 0x78, 0x7c, 0x7c,
//   0x80, 0x80, 0x84, 0x84, 0x88, 0x88, 0x8c, 0x8c,
//   0x90, 0x90, 0x94, 0x94, 0x98, 0x98, 0x9c, 0x9c,
//   0xa0, 0xa0, 0xa4, 0xa4, 0xa8, 0xa8, 0xac, 0xac,
//   0xb0, 0xb0, 0xb4, 0xb4, 0xb8, 0xb8, 0xbc, 0xbc,
//   0xc0, 0xc4, 0xc8, 0xcc, 0xd0, 0xd4, 0xd8, 0xdc,
//   0xe0, 0xe4, 0xe8, 0xec, 0xf0, 0xf4, 0xf8, 0xfc};

// var SITLZAH_HuffLength []uint8 = {
//     3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
//     3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
//     4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
//     4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
//     4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
//     5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
//     5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
//     5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
//     5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
//     6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
//     6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
//     6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
//     7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
//     7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
//     7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
//     8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8};

// type SITLZAHData struct {
// buf [4096]uint8
// Frequ [1000]uint32
// ForwTree [1000]uint32
// BackTree [1000]uint32
// };

// func  SITLZAH_move(uint32 *p, uint32 *q, uint32 n) void {
//   if(p > q)
//   {
//     while(n-- > 0)
//       *q++ = *p++;
//   }
//   else
//   {
//     p += n;
//     q += n;
//     while(n-- > 0)
//       *--q = *--p;
//   }
// }

// func  SIT_lzah(xadInOut *io) int32 {
// var i, i1, j, k, l, ch, offs, skip int32;
// var bufptr uint32 = 0;
//   var dat *SITLZAHData
//   var xadMasterBase *xadMasterBase = io.xio_xadMasterBase;

//   if((dat = (SITLZAHData *) xadAllocVec(XADM sizeof(SITLZAHData), XADMEMF_CLEAR|XADMEMF_PUBLIC)))
//   {
//     /* init buffer */
//     for(i = 0; i < SITLZAH_N; i++)
//     {
//       dat.Frequ[i] = 1;
//       dat.ForwTree[i] = i + SITLZAH_T;
//       dat.BackTree[i + SITLZAH_T] = i;
//     }
//     for(i = 0, j = SITLZAH_N; j < SITLZAH_T; i += 2, j++)
//     {
//       dat.Frequ[j] = dat.Frequ[i] + dat.Frequ[i + 1];
//       dat.ForwTree[j] = i;
//       dat.BackTree[i] = j;
//       dat.BackTree[i + 1] = j;
//     }
//     dat.Frequ[SITLZAH_T] = 0xffff;
//     dat.BackTree[SITLZAH_T - 1] = 0;

//     for(i = 0; i < 4096; i++)
//       dat.buf[i] = ' ';

//     while(!(io.xio_Flags & (XADIOF_LASTOUTBYTE|XADIOF_ERROR)))
//     {
//       ch = dat.ForwTree[SITLZAH_T - 1];
//       while(ch < SITLZAH_T)
//         ch = dat.ForwTree[ch + xadIOGetBitsHigh(io, 1)];
//       ch -= SITLZAH_T;
//       if(dat.Frequ[SITLZAH_T - 1] >= 0x8000) /* need to reorder */
//       {
//         j = 0;
//         for(i = 0; i < SITLZAH_T; i++)
//         {
//           if(dat.ForwTree[i] >= SITLZAH_T)
//           {
//             dat.Frequ[j] = ((dat.Frequ[i] + 1) >> 1);
//             dat.ForwTree[j] = dat.ForwTree[i];
//             j++;
//           }
//         }
//         j = SITLZAH_N;
//         for(i = 0; i < SITLZAH_T; i += 2)
//         {
//           k = i + 1;
//           l = dat.Frequ[i] + dat.Frequ[k];
//           dat.Frequ[j] = l;
//           k = j - 1;
//           while(l < dat.Frequ[k])
//             k--;
//           k = k + 1;
//           SITLZAH_move(dat.Frequ + k, dat.Frequ + k + 1, j - k);
//           dat.Frequ[k] = l;
//           SITLZAH_move(dat.ForwTree + k, dat.ForwTree + k + 1, j - k);
//           dat.ForwTree[k] = i;
//           j++;
//         }
//         for(i = 0; i < SITLZAH_T; i++)
//         {
//           k = dat.ForwTree[i];
//           if(k >= SITLZAH_T)
//             dat.BackTree[k] = i;
//           else
//           {
//             dat.BackTree[k] = i;
//             dat.BackTree[k + 1] = i;
//           }
//         }
//       }

//       i = dat.BackTree[ch + SITLZAH_T];
//       do
//       {
//         j = ++dat.Frequ[i];
//         i1 = i + 1;
//         if(dat.Frequ[i1] < j)
//         {
//           while(dat.Frequ[++i1] < j)
//             ;
//           i1--;
//           dat.Frequ[i] = dat.Frequ[i1];
//           dat.Frequ[i1] = j;

//           j = dat.ForwTree[i];
//           dat.BackTree[j] = i1;
//           if(j < SITLZAH_T)
//             dat.BackTree[j + 1] = i1;
//           dat.ForwTree[i] = dat.ForwTree[i1];
//           dat.ForwTree[i1] = j;
//           j = dat.ForwTree[i];
//           dat.BackTree[j] = i;
//           if(j < SITLZAH_T)
//             dat.BackTree[j + 1] = i;
//           i = i1;
//         }
//         i = dat.BackTree[i];
//       } while(i != 0);

//       if(ch < 256)
//       {
//         dat.buf[bufptr++] = xadIOPutChar(io, ch);
//         bufptr &= 0xFFF;
//       }
//       else
//       {
//         var byte = byte(xadIOGetBitsHigh(io, 8));
//         skip = SITLZAH_HuffLength[byte] - 2;
//         offs = (SITLZAH_HuffCode[byte]<<4) | (((byte << skip)  + xadIOGetBitsHigh(io, skip)) & 0x3f);
//         offs = ((bufptr - offs - 1) & 0xfff);
//         ch = ch - 253;
//         while(ch-- > 0)
//         {
//           dat.buf[bufptr++] = xadIOPutChar(io, dat.buf[offs++ & 0xfff]);
//           bufptr &= 0xFFF;
//         }
//       }
//     }
//     xadFreeObjectA(XADM dat, 0);
//   }
//   else
//     return XADERR_NOMEMORY;

//   return io.xio_Error;
// }
