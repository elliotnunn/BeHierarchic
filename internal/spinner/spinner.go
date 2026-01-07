// Copyright (c) Elliot Nunn
// Licensed under the MIT license

// Package spinner provides a cache for random-access reads to sequential-only files.
//
// Random access to a sequential file is achieved by closing, reopening and rereading
// the file when necessary.
//
// Performance is maintained by a cache of file blocks and a cache of open files.
package spinner

import (
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/dgryski/go-tinylfu"
)

// ReadAt reads len(p) bytes into p starting at offset off in the specified file.
// It has the semantics of [io.ReaderAt].
//
// The [Opener] must be a comparable type, that is, one that works with ==.
func ReadAt(id Opener, p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	c := make(chan readAtDone)
	readAtCalls <- readAtCall{id: id, p: p, off: off, done: c}
	d := <-c
	return d.n, d.err
}

type Opener interface {
	Open() (fs.File, error)
	fmt.Stringer // used for debug messages
}

const (
	blockSize   = 4096 // must match the AppleDouble resourcefork padding
	blockMask   = -blockSize
	blockCacheN = 1024 * 1024 * 1024 / blockSize

	readerCacheN = 64 // open readers (remember some decompressors carry big state buffers)

	becausePopular = 1
	becauseBusy    = 2
)

var (
	readAtCalls = make(chan readAtCall, 16)
	blockPool   = sync.Pool{New: func() any { return new(block) }}
	seed        = maphash.MakeSeed()
)

type (
	block [blockSize]byte

	readAtCall struct {
		id   Opener
		p    []byte
		off  int64
		done chan<- readAtDone
	}
	readAtDone struct {
		n   int
		err error
	}

	blockRequest struct {
		off int64
	}
	blockReturn struct {
		id  Opener
		p   *block
		off int64
		n   int
		err error
	}

	readAtState struct {
		readAtCall        // the struct we got from ReadAt
		progress   bitmap // the blocks that are still outstanding for this call
	}
	wkrState struct {
		ch      chan<- blockRequest
		seek    int64
		err     error
		errAt   int64
		whyKeep int
		readAts []readAtState
	}

	blkCacheKey struct {
		id     Opener
		offset int64
	}
)

func init() { go multiplexer() }
func multiplexer() {
	var (
		wkrs         = make(map[Opener]*wkrState)
		evictWkr     Opener
		blockReturns = make(chan blockReturn)
		blkCache     = tinylfu.New[blkCacheKey, *block](
			blockCacheN, blockCacheN*10, blkHash,
			tinylfu.OnEvict(blkEvict))
		wkrPopularity = tinylfu.New[Opener, struct{}](
			readerCacheN, readerCacheN*10, wkrHash,
			tinylfu.OnEvict(func(k Opener, _ struct{}) { evictWkr = k }))
	)
	for {
		var (
			wkr *wkrState
			id  Opener
		)
		var ticker <-chan time.Time
		// ticker = time.Tick(time.Second * 5)
		select {
		case <-ticker:
			for id, wk := range wkrs {
				fmt.Printf("State of %q: whyKeep=%v seek=%d err=%v@%d\n", id, wk.whyKeep, wk.seek, wk.err, wk.errAt)
				for _, r := range wk.readAts {
					fmt.Printf("    Pending read (%d,%d)", r.off, len(r.p))
				}
			}
			continue
		case job := <-readAtCalls:
			id, wkr = job.id, wkrs[job.id]
			if wkr == nil {
				wkr = new(wkrState)
				wkrs[job.id] = wkr
				ch := make(chan blockRequest, 1)
				wkr.ch = ch
				go work(id, ch, blockReturns)
				if knownSize, serr := sizeOf(id); serr == nil {
					wkr.err, wkr.errAt = io.EOF, knownSize
				}
			}

			wkrPopularity.Add(id, struct{}{}) // might set evictWkr
			wkr.whyKeep |= becausePopular
			if evictWkr != nil {
				exwkr := wkrs[evictWkr]
				exwkr.whyKeep &^= becausePopular
				if exwkr.whyKeep == 0 {
					close(exwkr.ch)
					delete(wkrs, evictWkr)
				}
			}
			evictWkr = nil

			r := readAtState{
				readAtCall: job,
				progress:   newBitmap(nBlocksTouched(job.off, job.p)),
			}
			for off := job.off & blockMask; off >= 0 && off < bufEnd(job.off, job.p); off += blockSize {
				if blk, ok := blkCache.Get(blkCacheKey{job.id, off}); ok {
					r.putBlock(off, blk)
				}
			}
			wkr.readAts = append(wkr.readAts, r)
		case done := <-blockReturns:
			id, wkr = done.id, wkrs[done.id]
			wkr.whyKeep &^= becauseBusy
			if done.off != wkr.seek {
				panic(fmt.Sprintf("did not get the block we requested: %d not %d", done.off, wkr.seek))
			}
			if done.p != nil {
				blkCache.Add(blkCacheKey{done.id, wkr.seek}, done.p)
				for i := range wkr.readAts {
					wkr.readAts[i].putBlock(wkr.seek, done.p)
				}
			}
			wkr.seek += int64(done.n)
			if done.err != nil {
				if done.err == io.EOF && wkr.err == io.EOF && wkr.errAt != wkr.seek {
					slog.Error("conflictingEOF", "path", id, "was", wkr.errAt, "becomes", wkr.seek)
				}
				wkr.err, wkr.errAt = done.err, wkr.seek
			}
		}

		// Return those readers that are fully satisfied (either all blocks retrieved, or an error found)
		keepReads := wkr.readAts[:0]
		for _, r := range wkr.readAts {
			furthestPossible := bufEnd(r.off, r.p) // that might be achieved in future iterations
			if wkr.err != nil {
				furthestPossible = min(furthestPossible, wkr.errAt)
			}

			furthestFound := furthestPossible // that we have achieved and put in the buffer so far
			if nextBit := r.progress.firstClear(0); nextBit >= 0 {
				furthestFound = min(furthestFound, offsetOfBlockIndex(r.off, nextBit))
			}

			if furthestFound == furthestPossible {
				if furthestFound == bufEnd(r.off, r.p) {
					r.done <- readAtDone{err: nil, n: len(r.p)}
				} else {
					n := furthestFound - r.off
					n = max(n, 0) // sanity clipping
					n = min(n, int64(len(r.p)))
					r.done <- readAtDone{err: wkr.err, n: int(n)}
				}
			} else { // leave for next time
				keepReads = append(keepReads, r)
			}
		}
		wkr.readAts = keepReads

		// now, finally, determine the direction that we must go in
		if wkr.whyKeep&becauseBusy != 0 {
			// just wait
		} else if len(wkr.readAts) == 0 {
			if wkr.whyKeep == 0 {
				close(wkr.ch)
				delete(wkrs, id)
			}
		} else {
			wantReset := true
			for _, r := range wkr.readAts {
				nextVacant := r.progress.firstClear(0)
				if nextVacant >= 0 && offsetOfBlockIndex(r.off, nextVacant) >= wkr.seek {
					wantReset = false
					break
				}
			}
			if wantReset {
				wkr.seek = 0
			}
			wkr.ch <- blockRequest{wkr.seek}
			wkr.whyKeep |= becauseBusy
		}
	}
}

func (r *readAtState) putBlock(off int64, p *block) {
	if off < r.off&blockMask || off >= bufEnd(r.off, r.p) {
		return // not applicable
	}
	bitmapIdx := int(off/blockSize - r.off/blockSize)
	r.progress.set(bitmapIdx)
	if off > r.off {
		copy(r.p[off-r.off:], p[:])
	} else {
		copy(r.p, p[r.off-off:])
	}
}

// work manages the lifecycle of a file (open-read-read-close)
// return when a closed ctrl channel indicates no further interest in the file
func work(id Opener, ctrl <-chan blockRequest, result chan<- blockReturn) {
	var (
		f   fs.File
		off int64
		err error
	)
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	for req := range ctrl {
		if req.off != 0 && req.off != off || (req.off == off && err != nil) {
			panic(fmt.Sprintf("invalid blockRequest for %d: %s", req.off, id))
		}

		if req.off < off {
			f.Close()
			f, off = nil, 0
		}

		if f == nil {
			f, err = id.Open()
			if err != nil {
				err = errWithPath(err, id.String())
				result <- blockReturn{id: id, off: 0, p: nil, n: 0, err: err}
				continue
			}
		}

		blk := blockPoolGet()
		n := 0
		for n < len(blk) && err == nil {
			var nn int
			nn, err = f.Read(blk[n:])
			n += nn
		}
		if n == 0 {
			blockPoolPut(blk)
			blk = nil
		}
		result <- blockReturn{id: id, off: off, p: blk, n: n, err: err}
		off += int64(n)
	}
}

func errWithPath(err error, path string) error {
	if err, ok := err.(*fs.PathError); ok {
		err.Path = path
		return err
	}
	return fmt.Errorf("%w: %s", err, path)
}

func blkHash(k blkCacheKey) uint64       { return maphash.Comparable(seed, k) }
func blkEvict(_ blkCacheKey, buf *block) { blockPoolPut(buf) }

func wkrHash(k Opener) uint64 { return maphash.Comparable(seed, k) }

func blockPoolGet() *block  { return blockPool.Get().(*block) }
func blockPoolPut(b *block) { blockPool.Put(b) }

func bufEnd(off int64, p []byte) int64 { return off + int64(len(p)) }

func nBlocksTouched(off int64, p []byte) int {
	return ((int(off) % blockSize) + len(p) + blockSize - 1) / blockSize
}

func offsetOfBlockIndex(bufoff int64, blockIdx int) int64 {
	if blockIdx == 0 {
		return bufoff
	}
	return bufoff&blockMask + int64(blockIdx)*blockSize
}
