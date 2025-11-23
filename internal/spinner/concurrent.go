// Package spinner converts sequential byte streams ([fs.File])
// to random-access byte collections ([io.ReaderAt]).
package spinner

import (
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/dgryski/go-tinylfu"
)

type Path interface { // keep in mind that this is used as a key
	Open() (fs.File, error)
	fmt.Stringer // likely to be used as a disk key, so choose well
}

// Create a new Pool with the specified properties.
func New(blockShift int, nBlock int, nReader int) *Pool {
	p := &Pool{
		jobs:    make(chan []*job),
		sizeQ:   make(chan *sizeQuery),
		shift:   blockShift,
		readers: make(map[Path]*readerState),
		dones:   make(chan Path),
		bufpool: sync.Pool{New: func() any { return unsafe.SliceData(make([]byte, 1<<blockShift)) }},
	}
	p.bcache = tinylfu.New[ckey, []byte](nBlock, nBlock*10, bhasher, tinylfu.OnEvict(p.bevict))
	p.rcache = tinylfu.New[Path, struct{}](nReader, nReader*10, rhasher, tinylfu.OnEvict(p.revict))
	go p.multiplexer()
	return p
}

// A Pool shares a configurable amount of memory among multiple "ReaderAt"s
// according to a caching algorithm.
// A Pool is safe for concurrent use by multiple goroutines.
type Pool struct {
	jobs    chan []*job
	sizeQ   chan *sizeQuery
	shift   int
	readers map[Path]*readerState
	dones   chan Path
	bcache  *tinylfu.T[ckey, []byte]
	rcache  *tinylfu.T[Path, struct{}]
	bufpool sync.Pool // use *byte rather than []byte to save slice-header allocations
}

// A ReaderAt is safe for concurrent use by multiple goroutines.
func (p *Pool) ReaderAt(f Path) ReaderAt {
	return ReaderAt{pool: p, id: f}
}

type ReaderAt struct {
	pool *Pool
	id   Path
}

func (r ReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	blksz := int64(1) << int64(r.pool.shift)
	var list []*job
	var b int64
	for b = off & -blksz; b < off+int64(len(p)); b += blksz {
		// for each block touching the request, PLUS one block!
		bufstart := max(b-off, 0)
		bufend := min(b+(1<<r.pool.shift)-off, int64(len(p)))
		list = append(list, &job{
			id:   r.id,
			p:    p[bufstart:bufend],
			off:  max(b, off),
			wait: make(chan struct{}),
		})
	}
	listPlusReadahead := append(list, &job{ // extra seek-ahead job
		id:   r.id,
		p:    make([]byte, 1),
		off:  b,
		wait: make(chan struct{}),
	})
	r.pool.jobs <- listPlusReadahead
	for _, j := range list {
		<-j.wait
		n += j.n
		if err == nil {
			err = j.err
			if err != nil {
				break
			}
		}
	}
	return n, err
}

func (r ReaderAt) SetSize(size int64) {
	if size < 0 {
		return
	}
	s := sizeQuery{
		id:       r.id,
		size:     size,
		knowsize: true,
		done:     make(chan struct{})}
	r.pool.sizeQ <- &s
	<-s.done // questionable whether we even need to wait
}

func (r ReaderAt) SizeIfCheap() (int64, bool) {
	s := sizeQuery{
		id:       r.id,
		knowsize: false,
		done:     make(chan struct{})}
	r.pool.sizeQ <- &s
	<-s.done // questionable whether we even need to wait
	return s.size, s.knowsize
}

func (r ReaderAt) SizeIfPossible() (size int64, ok bool) {
	defer func() {
		slog.Info("sizeIfPossible", "path", r.id, "size", size, "ok", ok)
	}()

	s := sizeQuery{
		id:       r.id,
		knowsize: false,
		done:     make(chan struct{})}
	r.pool.sizeQ <- &s
	<-s.done
	if s.knowsize {
		return s.size, true
	}

	// The cheap query failed, so try reading to the end of the file
	r.ReadAt(make([]byte, 1), math.MaxInt64-1000000) // prevents really unfortunate overflows
	s = sizeQuery{
		id:       r.id,
		knowsize: false,
		done:     make(chan struct{})}
	r.pool.sizeQ <- &s
	<-s.done
	return s.size, s.knowsize
}

// Best effort, may return 0 if the size is not knowable
func (r ReaderAt) Size() int64 {
	s, ok := r.SizeIfPossible()
	if !ok {
		return 0
	}
	return s
}

type sizeQuery struct {
	id       Path
	size     int64
	knowsize bool
	done     chan struct{}
}

type ckey struct {
	id     Path
	offset int64
}

// Part of a ReadAt request touching a single block
type job struct {
	id   Path
	p    []byte
	off  int64
	n    int
	err  error
	wait chan struct{}
}

// Track according to int-key. Create, never destroy.
type readerState struct {
	// Shared mutable state here, be careful
	fs.File
	data []byte
	err  error

	// Belongs only to the multiplexer goroutine
	busy    bool
	knowlen bool // means len field is exact
	diesoon bool // means remove from cache when done
	seek    int64
	len     int64 // lower bound on known length
	pending map[int64][]*job
}

func (p *Pool) multiplexer() {
	var pulse <-chan time.Time
	// pulse = time.Tick(time.Second * 15)
	for {
		select {
		case <-pulse:
			nQuiet := 0
			for path, r := range p.readers {
				if len(r.pending) == 0 {
					nQuiet++
					continue
				}
				slog.Info("activeReader", "path", path, "busy", r.busy, "diesoon", r.diesoon, "seek", r.seek)
				for offset, j := range r.pending {
					for _, j := range j {
						slog.Info("activeReaderJob", "offset", offset, "size", len(j.p))
					}
				}
			}
			slog.Info("quietReaders", "count", nQuiet)

		case joblist := <-p.jobs: // a ReadAt call
			for _, j := range joblist {
				// ensure we have a reader
				r := p.ensureReader(j.id)

				if r.knowlen && r.len <= j.off { // totally unsatisfiable, reject
					j.err, j.n = io.EOF, 0
					close(j.wait)
					continue
				}

				if canget := r.len - j.off; r.knowlen && canget < int64(len(j.p)) {
					j.err = io.EOF
				}

				blk := j.off >> p.shift << p.shift
				if got, ok := p.bcache.Get(ckey{j.id, blk}); ok {
					if j.off < blk+int64(len(got)) {
						j.n = copy(j.p, got[j.off-blk:])
					}
					close(j.wait)
				} else {
					if r.pending == nil {
						r.pending = make(map[int64][]*job)
					}
					r.pending[blk] = append(r.pending[blk], j)
					if !r.busy {
						p.startJob(j.id)
					}
				}
			}
		case q := <-p.sizeQ:
			r := p.ensureReader(q.id)
			if q.knowsize { // set
				r.knowlen, r.len = true, q.size
			} else { // get
				q.knowsize, q.size = r.knowlen, r.len
				// no effort made to cancel waiting ReaderAts, this would be a rare case
			}
			close(q.done)
		case id := <-p.dones: // a Reader goroutine has returned
			r := p.readers[id]
			p.bcache.Add(ckey{id, r.seek}, r.data)

			if r.err == io.EOF {
				r.knowlen, r.len = true, bufend(r.data, r.seek)
			}

			for _, j := range r.pending[r.seek] { // satisfy waiting ReaderAts
				if j.off < bufend(r.data, r.seek) {
					j.n = copy(j.p, r.data[j.off-r.seek:])
				}
				if j.n < len(j.p) {
					j.err = r.err
				}
				close(j.wait)
			}
			delete(r.pending, r.seek)

			// and if there is an error, dissatisfy other waiting ReaderAts
			if r.err != nil {
				for offset, jobs := range r.pending {
					if offset < r.seek {
						continue
					}
					for _, j := range jobs {
						j.err, j.n = r.err, 0
						close(j.wait)
					}
					delete(r.pending, offset)
				}
			}

			r.seek += int64(len(r.data))
			r.data = nil
			r.busy = false

			if len(r.pending) > 0 {
				p.startJob(id)
			} else {
				r.pending = nil
				if r.diesoon {
					go r.File.Close()
					r.File, r.diesoon = nil, false
				}
			}
		}
	}
}

func (p *Pool) startJob(id Path) {
	r, ok := p.readers[id]
	if !ok {
		panic("nonexistent reader")
	}
	r.busy, r.diesoon = true, false
	p.rcache.Add(id, struct{}{})

	worthPushingOn := false
	for offset := range r.pending {
		if offset >= r.seek {
			worthPushingOn = true
			break
		}
	}

	targetBlock := r.seek
	if r.File == nil || !worthPushingOn {
		targetBlock = 0
	}

	go func() {
		defer func() {
			p.dones <- id
		}()

		if r.File == nil || targetBlock == 0 && r.seek != 0 {
			if r.File != nil {
				go r.File.Close()
			}
			r.File, r.err = id.Open()
			if r.err != nil {
				return
			}
			r.seek = 0
		}

		var n int
		r.data = unsafe.Slice(p.bufpool.Get().(*byte), 1<<p.shift)
		n, r.err = io.ReadFull(r, r.data)
		if r.err == io.ErrUnexpectedEOF {
			r.err = io.EOF
		}
		r.data = r.data[:n]
	}()
}

func (p *Pool) bevict(k ckey, buf []byte) {
	p.bufpool.Put(unsafe.SliceData(buf))
}

// only ever called via startJob, so don't worry about sync
func (p *Pool) revict(id Path, _ struct{}) {
	r := p.readers[id]
	if r.busy {
		r.diesoon = true
	} else {
		if r.File != nil {
			go r.File.Close()
		}
		r.File = nil
	}
}

func (p *Pool) ensureReader(id Path) *readerState {
	r, ok := p.readers[id]
	if !ok {
		r = new(readerState)
		p.readers[id] = r
	}
	return r
}

func bufend(p []byte, off int64) int64 { return off + int64(len(p)) }

var seed = maphash.MakeSeed()

func bhasher(k ckey) uint64 {
	return maphash.Comparable(seed, k)
}

func rhasher(k Path) uint64 {
	return maphash.Comparable(seed, k)
}
