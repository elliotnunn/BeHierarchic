package decompressioncache

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"unsafe"
)

/*
Philosophical contemplation:
I should probably use a go interface to clarify things, something like this...

lets use a map, and semirandomly evict based on the pseudorandom map iteration order (hehe)
question is, how to ensure that sharedstate is kept around? -- actually it's not a huge deal, it can be
evicted the same as anything else...

hierarchy of saveable state...

      file initial state            = small
        /           \
 chunk state     chunk state        = small
	   |             |
 chunk content   chunk content      = large

and remember, it is okay to evict any kind of chunk, and we can do some weighting later...
eviction likely to be a "worst of a handful of randomly chosen keys" job

*/

type SpanDecompressor interface {
	// Both of these functions need to accept being called repeatedly
	Init(
		r io.ReaderAt,
		packsz int64, // -1 if unknown
		unpacksz int64, // -1 if unknown
	) (
		sharedstate []byte, // passed in to each subsequent
		err error, // if there is any error then we cannot decompress
	)

	Step(
		r io.ReaderAt,
		sharedstate []byte, // do not mutate!
		priorstate []byte, // allowed to mutate, but better just to return a fresh one
		wantnextstate bool,
	) (
		unpack []byte,
		ownstate []byte,
		nextstate []byte,
		err error,
	)
}

func New(d SpanDecompressor, packed io.ReaderAt, packsz, unpacksz int64, debugName string) Reader {
	bigLock.Lock()
	defer bigLock.Unlock()
	uniq := Reader(nextReaderHandle)
	nextReaderHandle++
	usedBytes += unsafe.Sizeof(file{})
	bigMap[uniq] = &file{
		engine:   d,
		packed:   packed,
		packsz:   packsz,
		unpacksz: unpacksz,
	}
	return uniq
}

type Reader uint64 // simply an opaque handle

var (
	bigLock sync.Mutex
	bigMap  = make(map[Reader]*file)
)

var usedBytes uintptr

const maxBytes = 1024 * 1024 * 1024 // a gigabyte should be plenty

type file struct {
	packed   io.ReaderAt
	engine   SpanDecompressor
	packsz   int64
	unpacksz int64
	atime    uint64
	state    []byte
	chunks   []chunk // could be nil if the file has been "kicked out"
	busy     int     // do not delete chunk list
}

type chunk struct {
	offset int64
	state  []byte // is compressed
	cache  []byte // can be nil, in which case it needs redoing
	atime  uint64
}

// Assumes bigLock is held
func freeSomeSpace(required int) {
	const (
		killChunkList = -1
		killState     = -2
	)
	max := maxBytes - uintptr(required)

	for usedBytes > max {
		var oldest Reader
		var oldestWhich int
		var oldestAtime uint64 = math.MaxUint64 - 1 // max reserved for "don't touch me"
		try := 10
		for r, f := range bigMap { // rely on Go maps being randomized, not a great idea
			if f.chunks == nil {
				continue
			}
			if f.atime <= oldestAtime {
				oldest = r
				oldestAtime = f.atime
				oldestWhich = killChunkList
				for i, ch := range f.chunks { // prefer to delete the stalest chunk
					if ch.cache != nil && ch.atime <= oldestAtime {
						oldestAtime = ch.atime
						oldestWhich = i
					}
				}
				if oldestWhich == killChunkList && f.busy > 0 {
					continue // not eligible for space-saving
				}
				if oldestWhich == killChunkList && f.chunks == nil {
					oldestWhich = killState
				}
			}

			try--
			if try == 0 {
				break
			}
		}

		if oldestAtime == math.MaxUint64-1 {
			panic("there is no space left to free!")
		}

		// Now delete what we decided to delete...
		switch oldestWhich {
		case killChunkList:
			usedBytes -= unsafe.Sizeof(chunk{}) * uintptr(cap(bigMap[oldest].chunks))
			for _, ch := range bigMap[oldest].chunks {
				usedBytes -= uintptr(cap(ch.state))
				usedBytes -= uintptr(cap(ch.cache))
			}
			bigMap[oldest].chunks = nil
		case killState:
			usedBytes -= uintptr(cap(bigMap[oldest].state))
			bigMap[oldest].state = nil
		default:
			usedBytes -= uintptr(cap(bigMap[oldest].chunks[oldestWhich].cache))
			bigMap[oldest].chunks[oldestWhich].cache = nil
		}
	}
}

func (r Reader) Size() int64 {
	bigLock.Lock()
	defer bigLock.Unlock()
	return bigMap[r].unpacksz
}

func (r Reader) ReadAt(p []byte, off int64) (int, error) {
	bigLock.Lock()
	havelock := true
	defer func() {
		if havelock {
			bigLock.Unlock()
		}
	}()

	var f *file = bigMap[r]
	f.busy++
	defer func() { f.busy-- }()
	f.atime = nextTimeIncrement
	nextTimeIncrement++

	if off >= f.unpacksz {
		return 0, io.EOF
	} else if off+int64(len(p)) > f.unpacksz {
		p = p[:f.unpacksz-off]
	}

	if len(f.chunks) == 0 {
		f.chunks = []chunk{{offset: 0}} // now need to size this one!
	}

	i := sort.Search(len(f.chunks), func(i int) bool {
		return f.chunks[i].offset > off
	}) - 1

	// start with the highest checkpoint that starts <= the request,
	for ; ; i++ {
		if f.chunks[i].cache == nil { // decompress a block expensively
			// relinquish the lock (this part is what takes time)
			if f.state == nil {
				s, err := f.engine.Init(f.packed, f.packsz, f.unpacksz)
				if err != nil {
					return 0, fmt.Errorf("%T.Init: %w", f.engine, err)
				}
				f.state = s
			}

			sEngine := f.engine
			sReader := f.packed
			sSharedState, sPriorState := f.state, f.chunks[i].state
			sWantNextState := i+1 == len(f.chunks)

			bigLock.Unlock()
			havelock = false

			unpack, ownstate, nextstate, err := sEngine.Step(sReader, sSharedState, sPriorState, sWantNextState)

			bigLock.Lock()
			havelock = true

			nextOffset := f.chunks[i].offset + int64(len(unpack))
			switch err {
			case nil:
			case io.EOF:
				if nextOffset != f.unpacksz {
					return 0, fmt.Errorf("%T.Step at offset %d: spurious io.EOF at size of %d (expected %d)",
						f.engine, f.chunks[i].offset, nextOffset, f.unpacksz)
				}
			default:
				return 0, fmt.Errorf("%T.Step at offset %d: %w",
					f.engine, f.chunks[i].offset, err)
			}
			if len(unpack) == 0 {
				return 0, fmt.Errorf("%T.Step returned zero bytes", f.engine)
			}

			// lots could have happened while we gave up the lock, although we are guaranteed that the chunk slice did not get shorter
			f.chunks[i].atime = f.atime
			f.chunks[i].cache = unpack // account for this in size!
			if ownstate != nil {
				f.chunks[i].state = ownstate // compress and all the rest...
			}

			if i+1 == len(f.chunks) && nextOffset < f.unpacksz {
				f.chunks = append(f.chunks, chunk{ // account for size here!
					offset: f.chunks[i].offset + int64(len(unpack)),
					state:  nextstate,
				})
			}
		}

		// copy bytes into the destination buffer
		destcut, srccut, _ := overlap(off, len(p), f.chunks[i].offset, len(f.chunks[i].cache))
		n := copy(p[destcut:], f.chunks[i].cache[srccut:])
		if destcut+n == len(p) {
			var err error
			if off+int64(len(p)) == f.unpacksz {
				err = io.EOF
			}
			return destcut + n, err
		}
	}
}

func overlap(aoffset int64, alen int, boffset int64, blen int) (ainner, binner int, ok bool) {
	if aoffset >= boffset+int64(blen) || boffset >= aoffset+int64(alen) {
		return 0, 0, false
	}

	if aoffset > boffset {
		binner = int(aoffset - boffset)
	} else {
		ainner = int(boffset - aoffset)
	}
	return ainner, binner, true
}

var nextReaderHandle, nextTimeIncrement uint64
