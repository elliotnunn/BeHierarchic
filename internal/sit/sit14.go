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

// type SIT14Data struct {
// io *xadInOut
// code [308]uint8
// codecopy [308]uint8
// freq [308]uint16
// buff [308]uint32

// var1 [52]uint8
// var2 [52]uint16
// var3 [75*2]uint16

// var4 [76]uint8
// var5 [75]uint32
// var6 [1024]uint8
// var7 [308*2]uint16
// var8 [0x4000]uint8

// Window [0x40000]uint8
// };

// func  SIT14_Update(uint16 first, uint16 last, uint8 *code, uint16 *freq) void {
// var i, j uint16

//   while(last-first > 1)
//   {
//     i = first;
//     j = last;

//     do
//     {
//       while(++i < last && code[first] > code[i])
//         ;
//       while(--j > first && code[first] < code[j])
//         ;
//       if(j > i)
//       {
// var t uint16;
//         t = code[i]; code[i] = code[j]; code[j] = t;
//         t = freq[i]; freq[i] = freq[j]; freq[j] = t;
//       }
//     } while(j > i);

//     if(first != j)
//     {
//       {
// var t uint16;
//         t = code[first]; code[first] = code[j]; code[j] = t;
//         t = freq[first]; freq[first] = freq[j]; freq[j] = t;
//       }

//       i = j+1;
//       if(last-i <= j-first)
//       {
//         SIT14_Update(i, last, code, freq);
//         last = j;
//       }
//       else
//       {
//         SIT14_Update(first, j, code, freq);
//         first = i;
//       }
//     }
//     else
//       ++first;
//   }
// }

// func  SIT14_ReadTree(SIT14Data *dat, uint16 codesize, uint16 *result) void {
// var size, i, j, k, l, m, n, o uint32

//   k = xadIOGetBitsLow(dat.io, 1);
//   j = xadIOGetBitsLow(dat.io, 2)+2;
//   o = xadIOGetBitsLow(dat.io, 3)+1;
//   size = 1<<j;
//   m = size-1;
//   k = k ? m-1 : -1;
//   if(xadIOGetBitsLow(dat.io, 2)&1) /* skip 1 bit! */
//   {
//     /* requirements for this call: dat.buff[32], dat.code[32], dat.freq[32*2] */
//     SIT14_ReadTree(dat, size, dat.freq);
//     for(i = 0; i < codesize; )
//     {
//       l = 0;
//       do
//       {
//         l = dat.freq[l + xadIOGetBitsLow(dat.io, 1)];
//         n = size<<1;
//       } while(n > l);
//       l -= n;
//       if(k != l)
//       {
//         if(l == m)
//         {
//           l = 0;
//           do
//           {
//             l = dat.freq[l + xadIOGetBitsLow(dat.io, 1)];
//             n = size<<1;
//           } while(n > l);
//           l += 3-n;
//           while(l--)
//           {
//             dat.code[i] = dat.code[i-1];
//             ++i;
//           }
//         }
//         else
//           dat.code[i++] = l+o;
//       }
//       else
//         dat.code[i++] = 0;
//     }
//   }
//   else
//   {
//     for(i = 0; i < codesize; )
//     {
//       l = xadIOGetBitsLow(dat.io, j);
//       if(k != l)
//       {
//         if(l == m)
//         {
//           l = xadIOGetBitsLow(dat.io, j)+3;
//           while(l--)
//           {
//             dat.code[i] = dat.code[i-1];
//             ++i;
//           }
//         }
//         else
//           dat.code[i++] = l+o;
//       }
//       else
//         dat.code[i++] = 0;
//     }
//   }

//   for(i = 0; i < codesize; ++i)
//   {
//     dat.codecopy[i] = dat.code[i];
//     dat.freq[i] = i;
//   }
//   SIT14_Update(0, codesize, dat.codecopy, dat.freq);

//   for(i = 0; i < codesize && !dat.codecopy[i]; ++i)
//     ; /* find first nonempty */
//   for(j = 0; i < codesize; ++i, ++j)
//   {
//     if(i)
//       j <<= (dat.codecopy[i] - dat.codecopy[i-1]);

//     k = dat.codecopy[i]; m = 0;
//     for(l = j; k--; l >>= 1)
//       m = (m << 1) | (l&1);

//     dat.buff[dat.freq[i]] = m;
//   }

//   for(i = 0; i < codesize*2; ++i)
//     result[i] = 0;

//   j = 2;
//   for(i = 0; i < codesize; ++i)
//   {
//     l = 0;
//     m = dat.buff[i];

//     for(k = 0; k < dat.code[i]; ++k)
//     {
//       l += (m&1);
//       if(dat.code[i]-1 <= k)
//         result[l] = codesize*2+i;
//       else
//       {
//         if(!result[l])
//         {
//           result[l] = j; j += 2;
//         }
//         l = result[l];
//       }
//       m >>= 1;
//     }
//   }
//   xadIOByteBoundary(dat.io);
// }

// func  SIT_14(xadInOut *io) int32 {
// var i j, k, l, m, n uint32
//   var xadMasterBase *xadMasterBase = io.xio_xadMasterBase;
//   var dat *SIT14Data

//   if((dat = (SIT14Data *) xadAllocVec(XADM sizeof(SIT14Data), XADMEMF_ANY|XADMEMF_CLEAR)))
//   {
//     dat.io = io;

//     /* initialization */
//     for(i = k = 0; i < 52; ++i)
//     {
//       dat.var2[i] = k;
//       k += (1<<(dat.var1[i] = ((i >= 4) ? ((i-4)>>2) : 0)));
//     }
//     for(i = 0; i < 4; ++i)
//       dat.var8[i] = i;
//     for(m = 1, l = 4; i < 0x4000; m <<= 1) /* i is 4 */
//     {
//       for(n = l+4; l < n; ++l)
//       {
//         for(j = 0; j < m; ++j)
//           dat.var8[i++] = l;
//       }
//     }
//     for(i = 0, k = 1; i < 75; ++i)
//     {
//       dat.var5[i] = k;
//       k += (1<<(dat.var4[i] = (i >= 3 ? ((i-3)>>2) : 0)));
//     }
//     for(i = 0; i < 4; ++i)
//       dat.var6[i] = i-1;
//     for(m = 1, l = 3; i < 0x400; m <<= 1) /* i is 4 */
//     {
//       for(n = l+4; l < n; ++l)
//       {
//         for(j = 0; j < m; ++j)
//           dat.var6[i++] = l;
//       }
//     }

//     m = xadIOGetBitsLow(io, 16); /* number of blocks */
//     j = 0; /* window position */
//     while(m-- && !(io.xio_Flags & (XADIOF_ERROR|XADIOF_LASTOUTBYTE)))
//     {
//       /* these functions do not support access > 24 bit */
//       xadIOGetBitsLow(io, 16); /* skip crunched block size */
//       xadIOGetBitsLow(io, 16);
//       n = xadIOGetBitsLow(io, 16); /* number of uncrunched bytes */
//       n |= xadIOGetBitsLow(io, 16)<<16;
//       SIT14_ReadTree(dat, 308, dat.var7);
//       SIT14_ReadTree(dat, 75, dat.var3);

//       while(n && !(io.xio_Flags & (XADIOF_ERROR|XADIOF_LASTOUTBYTE)))
//       {
//         for(i = 0; i < 616;)
//           i = dat.var7[i + xadIOGetBitsLow(io, 1)];
//         i -= 616;
//         if(i < 0x100)
//         {
//           dat.Window[j++] = xadIOPutChar(io, i);
//           j &= 0x3FFFF;
//           --n;
//         }
//         else
//         {
//           i -= 0x100;
//           k = dat.var2[i]+4;
//           i = dat.var1[i];
//           if(i)
//             k += xadIOGetBitsLow(io, i);
//           for(i = 0; i < 150;)
//             i = dat.var3[i + xadIOGetBitsLow(io, 1)];
//           i -= 150;
//           l = dat.var5[i];
//           i = dat.var4[i];
//           if(i)
//             l += xadIOGetBitsLow(io, i);
//           n -= k;
//           l = j+0x40000-l;
//           while(k--)
//           {
//             l &= 0x3FFFF;
//             dat.Window[j++] = xadIOPutChar(io, dat.Window[l++]);
//             j &= 0x3FFFF;
//           }
//         }
//       }
//       xadIOByteBoundary(io);
//     }
//     xadFreeObjectA(XADM dat, 0);
//   }
//   return io.xio_Error;
// }
