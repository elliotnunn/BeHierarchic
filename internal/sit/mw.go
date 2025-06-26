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

// type SITMWData struct {
// dict [16385]uint16
// stack [16384]uint16
// };

// func  SITMW_out(xadInOut *io, SITMWData *dat, int32 ptr) void {
// var stack_ptr uint16 = 1;

//   dat.stack[0] = ptr;
//   while(stack_ptr)
//   {
//     ptr = dat.stack[--stack_ptr];
//     while(ptr >= 256)
//     {
//       dat.stack[stack_ptr++] = dat.dict[ptr];
//       ptr = dat.dict[ptr - 1];
//     }
//     xadIOPutChar(io, (uint8) ptr);
//   }
// }

// func  SIT_mw(xadInOut *io) int32 {
//   var dat *SITMWData;
//   var xadMasterBase *xadMasterBase = io.xio_xadMasterBase;

//   if((dat = (SITMWData *) xadAllocVec(XADM sizeof(SITMWData), XADMEMF_CLEAR|XADMEMF_PUBLIC)))
//   {
// var ptr int32, max, max1, bits;

//     while(!(io.xio_Flags & (XADIOF_LASTOUTBYTE|XADIOF_ERROR)))
//     {
//       max = 256;
//       max1 = max << 1;
//       bits = 9;
//       ptr = xadIOGetBitsLow(io, bits);
//       if(ptr < max)
//       {
//         dat.dict[255] = ptr;
//         SITMW_out(io, dat, ptr);
//         while(!(io.xio_Flags & (XADIOF_LASTOUTBYTE|XADIOF_ERROR)) &&
//         (ptr = xadIOGetBitsLow(io, bits)) < max)
//         {
//           dat.dict[max++] = ptr;
//           if(max == max1)
//           {
//             max1 <<= 1;
//             bits++;
//           }
//           SITMW_out(io, dat, ptr);
//         }
//       }
//       if(ptr > max)
//         break;
//     }

//     xadFreeObjectA(XADM dat, 0);
//   }
//   else
//     return XADERR_NOMEMORY;

//   return io.xio_Error;
// }
