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

// static const uint16 SIT_rndtable[] = {
//  0xee,  0x56,  0xf8,  0xc3,  0x9d,  0x9f,  0xae,  0x2c,
//  0xad,  0xcd,  0x24,  0x9d,  0xa6, 0x101,  0x18,  0xb9,
//  0xa1,  0x82,  0x75,  0xe9,  0x9f,  0x55,  0x66,  0x6a,
//  0x86,  0x71,  0xdc,  0x84,  0x56,  0x96,  0x56,  0xa1,
//  0x84,  0x78,  0xb7,  0x32,  0x6a,   0x3,  0xe3,   0x2,
//  0x11, 0x101,   0x8,  0x44,  0x83, 0x100,  0x43,  0xe3,
//  0x1c,  0xf0,  0x86,  0x6a,  0x6b,   0xf,   0x3,  0x2d,
//  0x86,  0x17,  0x7b,  0x10,  0xf6,  0x80,  0x78,  0x7a,
//  0xa1,  0xe1,  0xef,  0x8c,  0xf6,  0x87,  0x4b,  0xa7,
//  0xe2,  0x77,  0xfa,  0xb8,  0x81,  0xee,  0x77,  0xc0,
//  0x9d,  0x29,  0x20,  0x27,  0x71,  0x12,  0xe0,  0x6b,
//  0xd1,  0x7c,   0xa,  0x89,  0x7d,  0x87,  0xc4, 0x101,
//  0xc1,  0x31,  0xaf,  0x38,   0x3,  0x68,  0x1b,  0x76,
//  0x79,  0x3f,  0xdb,  0xc7,  0x1b,  0x36,  0x7b,  0xe2,
//  0x63,  0x81,  0xee,   0xc,  0x63,  0x8b,  0x78,  0x38,
//  0x97,  0x9b,  0xd7,  0x8f,  0xdd,  0xf2,  0xa3,  0x77,
//  0x8c,  0xc3,  0x39,  0x20,  0xb3,  0x12,  0x11,   0xe,
//  0x17,  0x42,  0x80,  0x2c,  0xc4,  0x92,  0x59,  0xc8,
//  0xdb,  0x40,  0x76,  0x64,  0xb4,  0x55,  0x1a,  0x9e,
//  0xfe,  0x5f,   0x6,  0x3c,  0x41,  0xef,  0xd4,  0xaa,
//  0x98,  0x29,  0xcd,  0x1f,   0x2,  0xa8,  0x87,  0xd2,
//  0xa0,  0x93,  0x98,  0xef,   0xc,  0x43,  0xed,  0x9d,
//  0xc2,  0xeb,  0x81,  0xe9,  0x64,  0x23,  0x68,  0x1e,
//  0x25,  0x57,  0xde,  0x9a,  0xcf,  0x7f,  0xe5,  0xba,
//  0x41,  0xea,  0xea,  0x36,  0x1a,  0x28,  0x79,  0x20,
//  0x5e,  0x18,  0x4e,  0x7c,  0x8e,  0x58,  0x7a,  0xef,
//  0x91,   0x2,  0x93,  0xbb,  0x56,  0xa1,  0x49,  0x1b,
//  0x79,  0x92,  0xf3,  0x58,  0x4f,  0x52,  0x9c,   0x2,
//  0x77,  0xaf,  0x2a,  0x8f,  0x49,  0xd0,  0x99,  0x4d,
//  0x98, 0x101,  0x60,  0x93, 0x100,  0x75,  0x31,  0xce,
//  0x49,  0x20,  0x56,  0x57,  0xe2,  0xf5,  0x26,  0x2b,
//  0x8a,  0xbf,  0xde,  0xd0,  0x83,  0x34,  0xf4,  0x17
// };

// type SIT_modelsym struct
// {
// sym uint16
// cumfreq uint32
// };

// type SIT_model struct
// {
// increment int32
// maxfreq int32
// entries int32
// tabloc [256]uint32
// syms *SIT_modelsym
// };

// type SIT_ArsenicData struct
// {
// io *xadInOut

// csumaccum uint16
// window *uint8
// windowpos *uint8
// windowe *uint8
// windowsize int32
// tsize int32
// One uint32
// Half uint32
// Range uint32
// Code uint32
// lastarithbits int32 /* init 0 */

//   /* SIT_dounmntf function private */
// inited int32        /* init 0 */
// moveme [256]uint8

//   /* the private SIT_Arsenic function stuff */
//   initial_model SIT_model
//   selmodel SIT_model
// mtfmodel [7]SIT_model
// initial_syms [2+1]SIT_modelsym
// sel_syms [11+1]SIT_modelsym
// mtf0_syms [2+1]SIT_modelsym
// mtf1_syms [4+1]SIT_modelsym
// mtf2_syms [8+1]SIT_modelsym
// mtf3_syms [0x10+1]SIT_modelsym
// mtf4_syms [0x20+1]SIT_modelsym
// mtf5_syms [0x40+1]SIT_modelsym
// mtf6_syms [0x80+1]SIT_modelsym

//   /* private for SIT_unblocksort */
// counts [256]uint32
// cumcounts [256]uint32
// };

// func  SIT_update_model(SIT_model *mymod, int32 symindex) void {
// var i int32;

//   for (i = 0; i < symindex; i++)
//     mymod.syms[i].cumfreq += mymod.increment;
//   if(mymod.syms[0].cumfreq > mymod.maxfreq)
//   {
//     for(i = 0; i < mymod.entries ; i++)
//     {
//       /* no -1, want to include the 0 entry */
//       /* this converts cumfreqs LONGo frequencies, then shifts right */
//       mymod.syms[i].cumfreq -= mymod.syms[i+1].cumfreq;
//       mymod.syms[i].cumfreq++; /* avoid losing things entirely */
//       mymod.syms[i].cumfreq >>= 1;
//     }
//     /* then convert frequencies back to cumfreq */
//     for(i = mymod.entries - 1; i >= 0; i--)
//       mymod.syms[i].cumfreq += mymod.syms[i+1].cumfreq;
//   }
// }

// static void SIT_getcode(SIT_ArsenicData *sa,
// uint32 symhigh, uint32 symlow, uint32 symtot) /* aka remove symbol */
// {
// var lowincr uint32;
// var renorm_factor uint32;

//   renorm_factor = sa.Range/symtot;
//   lowincr = renorm_factor * symlow;
//   sa.Code -= lowincr;
//   if(symhigh == symtot)
//     sa.Range -= lowincr;
//   else
//     sa.Range = (symhigh - symlow) * renorm_factor;

//   sa.lastarithbits = 0;
//   while(sa.Range <= sa.Half)
//   {
//     sa.Range <<= 1;
//     sa.Code = (sa.Code << 1) | xadIOGetBitsHigh(sa.io, 1);
//     sa.lastarithbits++;
//   }
// }

// func  SIT_getsym(SIT_ArsenicData *sa, SIT_model *model) int32 {
// var freq int32;
// var i int32;
// var sym int32;

//   /* getfreq */
//   freq = sa.Code/(sa.Range/model.syms[0].cumfreq);
//   for(i = 1; i < model.entries; i++)
//   {
//     if(model.syms[i].cumfreq <= freq)
//       break;
//   }
//   sym = model.syms[i-1].sym;
//   SIT_getcode(sa, model.syms[i-1].cumfreq, model.syms[i].cumfreq, model.syms[0].cumfreq);
//   SIT_update_model(model, i);
//   return sym;
// }

// func  SIT_reinit_model(SIT_model *mymod) void {
// var cumfreq int32 = mymod.entries * mymod.increment;
// var i int32;

//   for(i = 0; i <= mymod.entries; i++)
//   {
//     /* <= sets last frequency to 0; there isn't really a symbol for that
//        last one  */
//     mymod.syms[i].cumfreq = cumfreq;
//     cumfreq -= mymod.increment;
//   }
// }

// static void SIT_init_model(SIT_model *newmod, SIT_modelsym *sym,
// int32 entries, int32 start, int32 increment, int32 maxfreq)
// {
// var i int32;

//   newmod.syms = sym;
//   newmod.increment = increment;
//   newmod.maxfreq = maxfreq;
//   newmod.entries = entries;
//   /* memset(newmod.tabloc, 0, sizeof(newmod.tabloc)); */
//   for(i = 0; i < entries; i++)
//   {
//     newmod.tabloc[(entries - i - 1) + start] = i;
//     newmod.syms[i].sym = (entries - i - 1) + start;
//   }
//   SIT_reinit_model(newmod);
// }

// func  SIT_arith_getbits(SIT_ArsenicData *sa, SIT_model *model, int32 nbits) uint32 {
//   /* the model is assumed to be a binary one */
// var addme uint32 = 1;
// var accum uint32 = 0;
//   while(nbits--)
//   {
//     if(SIT_getsym(sa, model))
//       accum += addme;
//     addme += addme;
//   }
//   return accum;
// }

// func  SIT_dounmtf(SIT_ArsenicData *sa, int32 sym) int32 {
// var i int32;
// var result int32;

//   if(sym == -1 || !sa.inited)
//   {
//     for(i = 0; i < 256; i++)
//       sa.moveme[i] = i;
//     sa.inited = 1;
//   }
//   if(sym == -1)
//     return 0;
//   result = sa.moveme[sym];
//   for(i = sym; i > 0 ; i-- )
//     sa.moveme[i] = sa.moveme[i-1];

//   sa.moveme[0] = result;
//   return result;
// }

// static int32 SIT_unblocksort(SIT_ArsenicData *sa, uint8 *block,
// uint32 blocklen, uint32 last_index, uint8 *outblock)
// {
// var i uint32, j;
//   uint32 *xform;
//   uint8 *blockptr;
// var cum uint32;
//   xadMasterBase *xadMasterBase = sa.io.xio_xadMasterBase;

//   memset(sa.counts, 0, sizeof(sa.counts));
//   if((xform = xadAllocVec(XADM sizeof(uint32)*blocklen, XADMEMF_ANY)))
//   {
//     blockptr = block;
//     for(i = 0; i < blocklen; i++)
//       sa.counts[*blockptr++]++;

//     cum = 0;
//     for(i = 0; i < 256; i++)
//     {
//       sa.cumcounts[i] = cum;
//       cum += sa.counts[i];
//       sa.counts[i] = 0;
//     }

//     blockptr = block;
//     for(i = 0; i < blocklen; i++)
//     {
//       xform[sa.cumcounts[*blockptr] + sa.counts[*blockptr]] = i;
//       sa.counts[*blockptr++]++;
//     }

//     blockptr = outblock;
//     for(i = 0, j = xform[last_index]; i < blocklen; i++, j = xform[j])
//     {
//       *blockptr++ = block[j];
// //      block[j] = 0xa5; /* for debugging */
//     }
//     xadFreeObjectA(XADM xform, 0);
//   }
//   else
//     return XADERR_NOMEMORY;
//   return 0;
// }

// func  SIT_write_and_unrle_and_unrnd(xadInOut *io, uint8 *block, uint32 blocklen, int16 rnd) void {
// var count int32 = 0;
// var last int32 = 0;
//   var blockptr *uint8 = block;
// var i uint32;
// var j uint32;
// var ch int32;
// var rndindex int32;
// var rndcount int32;

//   rndindex = 0;
//   rndcount = SIT_rndtable[rndindex];
//   for(i = 0; i < blocklen; i++)
//   {
//     ch = *blockptr++;
//     if(rnd && (rndcount == 0))
//     {
//       ch ^= 1;
//       rndindex++;
//       if (rndindex == sizeof(SIT_rndtable)/sizeof(SIT_rndtable[0]))
//         rndindex = 0;
//       rndcount = SIT_rndtable[rndindex];
//     }
//     rndcount--;

//     if(count == 4)
//     {
//       for(j = 0; j < ch; j++)
//         xadIOPutChar(io, last);
//       count = 0;
//     }
//     else
//     {
//       xadIOPutChar(io, ch);
//       if(ch != last)
//       {
//         count = 0;
//         last = ch;
//       }
//       count++;
//     }
//   }
// }

// func  SIT_Arsenic(xadInOut *io) int32 {
// var err int32 = 0;
//   var sa *SIT_ArsenicData
//   var xadMasterBase *xadMasterBase = io.xio_xadMasterBase;

//   io.xio_Flags &= ~(XADIOF_NOCRC32);
//   io.xio_Flags |= XADIOF_NOCRC16;
//   io.xio_CRC32 = ~0;

//   if((sa = (SIT_ArsenicData *) xadAllocVec(XADM sizeof(SIT_ArsenicData), XADMEMF_ANY|XADMEMF_CLEAR)))
//   {
// var i int32, sym, sel;
// var blockbits int16;
// var w uint32, blocksize;
// var stopme int32, nchars; /* 32 bits */
// var repeatstate int32, repeatcount;
// var primary_index int32; /* 32 bits */
// var eob int32, rnd;
//     uint8 *block, *blockptr, *unsortedblock;

//     sa.io = io;
//     sa.Range = sa.One = 1<<25;
//     sa.Half = 1<<24;
//     sa.Code = xadIOGetBitsHigh(io, 26);

//     SIT_init_model(&sa.initial_model, sa.initial_syms, 2, 0, 1, 256);
//     SIT_init_model(&sa.selmodel, sa.sel_syms, 11, 0, 8, 1024);
//     /* selector model: 11 selections, starting at 0, 8 increment, 1024 maxfreq */

//     SIT_init_model(&sa.mtfmodel[0], sa.mtf0_syms, 2, 2, 8, 1024);
//     /* model 3: 2 symbols, starting at 2, 8 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[1], sa.mtf1_syms, 4, 4, 4, 1024);
//     /* model 4: 4 symbols, starting at 4, 4 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[2], sa.mtf2_syms, 8, 8, 4, 1024);
//     /* model 5: 8 symbols, starting at 8, 4 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[3], sa.mtf3_syms, 0x10, 0x10, 4, 1024);
//     /* model 6: $10 symbols, starting at $10, 4 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[4], sa.mtf4_syms, 0x20, 0x20, 2, 1024);
//     /* model 7: $20 symbols, starting at $20, 2 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[5], sa.mtf5_syms, 0x40, 0x40, 2, 1024);
//     /* model 8: $40 symbols, starting at $40, 2 increment, 1024 maxfreq */
//     SIT_init_model(&sa.mtfmodel[6], sa.mtf6_syms, 0x80, 0x80, 1, 1024);
//     /* model 9: $80 symbols, starting at $80, 1 increment, 1024 maxfreq */
//     if(SIT_arith_getbits(sa, &sa.initial_model, 8) != 0x41 ||
//     SIT_arith_getbits(sa, &sa.initial_model, 8) != 0x73)
//       err = XADERR_ILLEGALDATA;
//     w = SIT_arith_getbits(sa, &sa.initial_model, 4);
//     blockbits = w + 9;
//     blocksize = 1<<blockbits;
//     if(!err)
//     {
//       if((block = xadAllocVec(XADM blocksize, XADMEMF_ANY)))
//       {
//         if((unsortedblock = xadAllocVec(XADM blocksize, XADMEMF_ANY)))
//         {
//           eob = SIT_getsym(sa, &sa.initial_model);
//           while(!eob && !err)
//           {
//             rnd = SIT_getsym(sa, &sa.initial_model);
//             primary_index = SIT_arith_getbits(sa, &sa.initial_model, blockbits);
//             nchars = stopme = repeatstate = repeatcount = 0;
//             blockptr = block;
//             while(!stopme)
//             {
//               sel = SIT_getsym(sa, &sa.selmodel);
//               switch(sel)
//               {
//               case 0:
//                 sym = -1;
//                 if(!repeatstate)
//                   repeatstate = repeatcount = 1;
//                 else
//                 {
//                   repeatstate += repeatstate;
//                   repeatcount += repeatstate;
//                 }
//                 break;
//               case 1:
//                 if(!repeatstate)
//                 {
//                   repeatstate = 1;
//                   repeatcount = 2;
//                 }
//                 else
//                 {
//                   repeatstate += repeatstate;
//                   repeatcount += repeatstate;
//                   repeatcount += repeatstate;
//                 }
//                 sym = -1;
//                 break;
//               case 2:
//                 sym = 1;
//                 break;
//               case 10:
//                 stopme = 1;
//                 sym = 0;
//                 break;
//               default:
//                 if((sel > 9) || (sel < 3))
//                 { /* this basically can't happen */
//                   err = XADERR_ILLEGALDATA;
//                   stopme = 1;
//                   sym = 0;
//                 }
//                 else
//                   sym = SIT_getsym(sa, &sa.mtfmodel[sel-3]);
//                 break;
//               }
//               if(repeatstate && (sym >= 0))
//               {
//                 nchars += repeatcount;
//                 repeatstate = 0;
//                 memset(blockptr, SIT_dounmtf(sa, 0), repeatcount);
//                 blockptr += repeatcount;
//                 repeatcount = 0;
//               }
//               if(!stopme && !repeatstate)
//               {
//                 sym = SIT_dounmtf(sa, sym);
//                 *blockptr++ = sym;
//                 nchars++;
//               }
//               if(nchars > blocksize)
//               {
//                 err = XADERR_ILLEGALDATA;
//                 stopme = 1;
//               }
//             }
//             if(err)
//               break;
//             if((err = SIT_unblocksort(sa, block, nchars, primary_index, unsortedblock)))
//               break;
//             SIT_write_and_unrle_and_unrnd(io, unsortedblock, nchars, rnd);
//             eob = SIT_getsym(sa, &sa.initial_model);
//             SIT_reinit_model(&sa.selmodel);
//             for(i = 0; i < 7; i ++)
//               SIT_reinit_model(&sa.mtfmodel[i]);
//             SIT_dounmtf(sa, -1);
//           }
//           if(!err)
//           {
//             err = xadIOWriteBuf(io);
//             if(!err && SIT_arith_getbits(sa, &sa.initial_model, 32) != ~io.xio_CRC32)
//               err = XADERR_CHECKSUM;
//           }
//           xadFreeObjectA(XADM unsortedblock, 0);
//         }
//         else
//           err = XADERR_NOMEMORY;
//         xadFreeObjectA(XADM block, 0);
//       }
//       else
//         err = XADERR_NOMEMORY;
//     } /* if(!err) */
//     xadFreeObjectA(XADM sa, 0);
//   }
//   else
//     err = XADERR_NOMEMORY;

//   return err;
// }
