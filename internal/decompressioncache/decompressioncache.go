package decompressioncache

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync/atomic"

	"github.com/allegro/bigcache/v3"
)

type Stepper func() (Stepper, []byte, error)

func New(stepper Stepper, size int64, debugName string) *ReaderAt {
	return &ReaderAt{
		uniq:        atomic.AddUint64(&monotonic, 1),
		debugName:   debugName,
		checkpoints: []checkpoint{{stepper: stepper, offset: 0}},
		size:        size,
	}
}

func (r *ReaderAt) Size() int64 {
	return r.size
}

func (r *ReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.size {
		println("decompressioncache eof")
		return 0, io.EOF
	} else if off+int64(len(p)) > r.size {
		println("decompressioncache near eof")
		p = p[:r.size-off]
	}

	println("having a crack at a read")
	i := sort.Search(len(r.checkpoints), func(i int) bool {
		return r.checkpoints[i].offset > off
	}) - 1

	// start with the highest checkpoint that starts <= the request
	for { // with some care this loop could be concurrent
		key := fmt.Sprintf("%s_%d_%d", r.debugName, r.uniq, r.checkpoints[i].offset)
		blob, cacheErr := cache.Get(key)

		if cacheErr != nil { // decompress a block expensively
			newstepper, newblob, err := r.checkpoints[i].stepper()
			blob = newblob
			cache.Set(key, blob)
			if i+1 == len(r.checkpoints) {
				r.checkpoints = append(r.checkpoints, checkpoint{
					stepper: newstepper,
					offset:  r.checkpoints[i].offset + int64(len(blob))})
			}
			r.checkpoints[i].err = err
		}

		// copy bytes into the destination buffer
		destcut, srccut, ok := overlap(off, len(p), r.checkpoints[i].offset, len(blob))
		if ok {
			n := copy(p[destcut:], blob[srccut:])
			if destcut+n == len(p) /*satisfied*/ || r.checkpoints[i].err != nil /*eof*/ {
				return destcut + n, r.checkpoints[i].err
			}
		}

		i++
	}
}

type ReaderAt struct {
	uniq        uint64
	debugName   string
	checkpoints []checkpoint // once there is no more data, nil checkpoint
	size        int64
}

type checkpoint struct {
	stepper Stepper
	offset  int64
	err     error
}

var monotonic uint64

var cache *bigcache.BigCache

func init() {
	c, err := bigcache.New(context.Background(), bigcache.Config{
		HardMaxCacheSize: 1024, // megabytes
		Shards:           1024,
	})
	if err != nil {
		panic(err)
	}
	cache = c
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
