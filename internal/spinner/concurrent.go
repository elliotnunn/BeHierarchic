// Package spinner converts sequential byte streams ([fs.File])
// to random-access byte collections ([io.ReaderAt]).
package spinner

import (
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	"math"

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
		sizeQ:   make(chan sizeQuery),
		sizeS:   make(chan sizeSet),
		shift:   blockShift,
		readers: make(map[Path]*readerState),
		dones:   make(chan Path),
		bcache:  tinylfu.New[ckey, []byte](nBlock, nBlock*10, bhasher),
	}
	p.rcache = tinylfu.New[Path, struct{}](nReader, nReader*10, rhasher, tinylfu.OnEvict(p.evict))
	go p.multiplexer()
	return p
}

// A Pool shares a configurable amount of memory among multiple "ReaderAt"s
// according to a caching algorithm.
// A Pool is safe for concurrent use by multiple goroutines.
type Pool struct {
	jobs    chan []*job
	sizeQ   chan sizeQuery
	sizeS   chan sizeSet
	shift   int
	readers map[Path]*readerState
	dones   chan Path
	bcache  *tinylfu.T[ckey, []byte]
	rcache  *tinylfu.T[Path, struct{}]
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

func (r ReaderAt) Size() int64 {
	q := sizeQuery{r.id, make(chan int64)}

	// Try to query our cache about the file size cheaply
	r.pool.sizeQ <- q
	if s := <-q.ret; s != -1 {
		return s
	}

	// Then try reading to the end of the file
	r.ReadAt(make([]byte, 1), math.MaxInt64-1000000) // prevents really unfortunate overflows

	// Then query our cache again
	r.pool.sizeQ <- q
	if s := <-q.ret; s != -1 {
		return s
	} else {
		return 0 // give up
	}
}

func (r ReaderAt) SetSize(size int64) {
	s := sizeSet{id: r.id, size: size, done: make(chan struct{})}
	r.pool.sizeS <- s
	<-s.done
}

type sizeQuery struct {
	id  Path
	ret chan int64
}

type sizeSet struct {
	id   Path
	size int64
	done chan struct{}
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
	for {
		select {
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
		case q := <-p.sizeQ: // call to Size()
			r := p.ensureReader(q.id)
			if r.knowlen {
				q.ret <- p.readers[q.id].len
			} else {
				q.ret <- -1
			}
		case s := <-p.sizeS: // call to SetSize()
			r := p.ensureReader(s.id)
			r.knowlen, r.len = true, s.size
			close(s.done)
			// no effort made to cancel waiting ReaderAts, this would be a rare case
		case id := <-p.dones: // a Reader goroutine has returned
			r := p.readers[id]
			p.bcache.Add(ckey{id, r.seek}, r.data)

			for _, j := range r.pending[r.seek] { // satisfy waiting ReaderAts
				if j.off < r.seek+int64(len(r.data)) {
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
				if r.err == io.EOF {
					r.knowlen, r.len = true, r.seek+int64(len(r.data))
				}
				for offset, jobs := range r.pending {
					if offset >= r.len {
						for _, j := range jobs {
							j.err, j.n = r.err, 0
							close(j.wait)
						}
						delete(r.pending, offset)
					}
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
		r.data = make([]byte, 1<<p.shift)
		n, r.err = io.ReadFull(r, r.data)
		if r.err == io.ErrUnexpectedEOF {
			r.err = io.EOF
		}
		r.data = smooshBuffer(r.data[:n])
	}()
}

// only ever called via startJob, so don't worry about sync
func (p *Pool) evict(id Path, _ struct{}) {
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

// Use a smaller memory block
func smooshBuffer(buf []byte) []byte {
	if len(buf) == 0 {
		return nil
	} else if len(buf) <= cap(buf)/2 {
		return append(make([]byte, 0, len(buf)), buf...)
	} else {
		return buf
	}
}

var seed = maphash.MakeSeed()

func bhasher(k ckey) uint64 {
	return maphash.Comparable(seed, k)
}

func rhasher(k Path) uint64 {
	return maphash.Comparable(seed, k)
}
