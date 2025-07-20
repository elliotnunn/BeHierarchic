package spinner

import (
	"fmt"
	"io"
	"iter"
)

const blocksize = 1 << 10

type ID int

type job struct {
	id   ID
	p    []byte // never more than a
	off  int64
	n    int
	err  error
	wait chan struct{}
}

type result struct {
	n   int
	err error
}

// Set this up with some injection...
var OpenFunc func(id ID) (io.Reader /*can be ReadCloser*/, error)

var (
	jobs = make(chan []*job)
)

// Cancellation (e.g. with a Close) might be good here
// Hang on, why don't I just submit a very large number of single-block jobs?
// (Problem is that this would be somewhat racy, which is kind of a nuisance...)
func (id ID) ReadAt(p []byte, off int64) (n int, err error) {
	list := make([]*job, 0, nBlocks(off, len(p)))
	for b := range listBlocks(off, len(p)) {
		bufstart := max(b-off, 0)
		bufend := min(b+blocksize-off, int64(len(p)))
		list = append(list, &job{
			id:   id,
			p:    p[bufstart:bufend],
			off:  max(b, off),
			wait: make(chan struct{}),
		})
	}
	println("len list", len(list))
	jobs <- list
	for _, j := range list {
		<-j.wait
		n += j.n
		if j.n < len(j.p) {
			err = j.err
			if err == nil {
				panic("err unexpectedly nil")
			}
			break
		}
	}
	return n, err
}

// temporary simplification: the only error ever returned is io.eof, IFF the number of bytes is < the request
// nah, all errors are permanent!
func multiplexer() {
	type readerState struct {
		io.Reader
		seek    int64
		data    []byte
		busy    bool
		err     error
		pending map[int64][]*job
	}
	type knownErr struct { // also provides backing for Size business (which is valuable information!)
		err    error
		offset int64
	}
	var (
		dones   = make(chan ID)
		readers = make(map[ID]*readerState)
		// errors  = make(map[ID]knownErr)
	)

	startJob := func(id ID) {
		r := readers[id]
		r.busy = true

		worthPushingOn := false
		for offset := range r.pending {
			if offset >= r.seek {
				worthPushingOn = true
				break
			}
		}

		targetBlock := r.seek
		if r.Reader == nil || !worthPushingOn {
			targetBlock = 0
		}

		go func() {
			defer func() { dones <- id }()

			if r.Reader == nil || targetBlock == 0 && r.seek != 0 {
				if closer, ok := r.Reader.(io.Closer); ok {
					go closer.Close()
				}
				r.Reader, r.err = OpenFunc(id)
				if r.err != nil {
					return
				}
				r.seek = 0
			}

			var n int
			r.data = make([]byte, blocksize)
			n, r.err = io.ReadFull(r, r.data)
			r.data = smooshBuffer(r.data[:n])
		}()
	}

	for {
		select {
		case joblist := <-jobs:
			if len(joblist) == 0 {
				panic("should never happen")
			}
			// short-circuit jobs where the error is known (but there is no data!)
			r, ok := readers[joblist[0].id]
			if !ok {
				r = &readerState{pending: make(map[int64][]*job)}
				readers[joblist[0].id] = r
			}
			for _, j := range joblist {
				r.pending[j.off&-blocksize] = append(r.pending[j.off&-blocksize], j)
			}
			if !r.busy {
				startJob(joblist[0].id)
			}
		case id := <-dones:
			r := readers[id]
			fmt.Printf("satisfying %d jobs\n", len(r.pending[r.seek]))
			for _, j := range r.pending[r.seek] {
				j.n = copy(j.p, r.data[j.off-r.seek:])
				j.err = r.err
				close(j.wait)
			}
			delete(r.pending, r.seek)
			r.seek += blocksize // is that actually reliable??
			r.busy = false
			if len(r.pending) > 0 {
				startJob(id)
			}
		}
	}
}

func smooshBuffer(buf []byte) []byte {
	if len(buf) == 0 {
		return nil
	} else if len(buf) <= cap(buf)/2 {
		return append(make([]byte, 0, len(buf)), buf...)
	} else {
		return buf
	}
}

func listBlocks(offset int64, length int) iter.Seq[int64] {
	return func(yield func(int64) bool) {
		for i := offset & -blocksize; i < offset+int64(length); i += blocksize {
			if !yield(i) {
				return
			}
		}
	}
}

func nBlocks(offset int64, length int) int {
	return int((offset+int64(length)+blocksize-1)/blocksize - offset/blocksize)
}

func init() {
	go multiplexer()
}
