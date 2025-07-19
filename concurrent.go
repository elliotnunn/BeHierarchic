package main

import (
	"io"
)

const blocksize = 1 << 15 // 32 KiB

type ID int

type i1msg struct {
	key                ID
	firstBlock, nBlock int64
	reply              chan o1msg // promise of nBlock messages unless there is an error
}

type o1msg struct {
	which int64
	block []byte
	err   error
}

type o2msg struct {
	key   ID
	which int64
	block []byte
	err   error
}

type i2msg struct {
	firstBlock, nBlock int64
}

var (
	ch1 = make(chan i1msg)
	ch2 = make(chan o2msg)
)

// Set this up with some injection...
var OpenFunc func(id ID) (io.Reader /*can be ReadCloser*/, error)

// Cancellation (e.g. with a Close) might be good here
func (id ID) ReadAt(p []byte, off int64) (n int, err error) {
	i := i1msg{
		firstBlock: off / blocksize,
		nBlock:     (off+int64(n)+blocksize-1)/blocksize - off/blocksize,
		reply:      make(chan o1msg),
	}
	for o := range i.reply {
		if o.err != nil {
			err = o.err
		}
		if o.which*blocksize < off {
			n += copy(p, o.block[off-o.which*blocksize:])
		} else {
			n += copy(p[o.which*blocksize-off:], o.block)
		}
	}
	return
}

func multiplexer() {
	type blkid struct {
		file  ID
		block int64
	}
	workers := make(map[ID]chan i2msg)
	outstandingJobs := make(map[blkid]map[chan o1msg]struct{})
	retchans := make(map[chan o1msg]int64) // the number of block returns needed for that channel

	for {
		select {
		case i := <-ch1: // ReadAt call
			channel, ok := workers[i.key]
			if !ok {
				channel = make(chan i2msg)
				workers[i.key] = channel
				go organizer(i.key, channel) // need to know when to quiesce this one...
				// after all that's the whole point of having a goroutine like this
			}
			channel <- i2msg{
				firstBlock: i.firstBlock,
				nBlock:     i.nBlock,
			}
			for b := i.firstBlock; b < i.firstBlock+i.nBlock; b++ {
				blkid := blkid{i.key, b}
				if outstandingJobs[blkid] == nil {
					outstandingJobs[blkid] = make(map[chan o1msg]struct{})
				}
				outstandingJobs[blkid][i.reply] = struct{}{}
			}
			retchans[i.reply] = i.nBlock
		case o := <-ch2: // block has been calculated
			interestedJobs := outstandingJobs[blkid{o.key, o.which}]
			delete(outstandingJobs, blkid{o.key, o.which})
			for channel := range interestedJobs {
				channel <- o1msg{
					which: o.which,
					block: o.block,
					err:   o.err,
				}
				retchans[channel]--
				if retchans[channel] == 0 {
					close(channel)
					delete(retchans, channel)
				}
			}
		}
	}
}

// Up to one of these per ID, should always accept new work and never block
func organizer(id ID, ch chan i2msg) {
	var rdr io.Reader
	todo := make(map[int64]struct{})
	var worker chan int64
	seek := int64(0)
	for {
		select {
		case extent := <-ch:
			setBlockList(todo, extent.firstBlock, extent.nBlock)
		case block, ok := <-worker:
			if ok {
				delete(todo, block)
			} else {
				worker = nil
			}
		}
		if worker != nil || len(todo) == 0 {
			continue
		}

		seekto := int64(1)
		for block := range todo {
			seekto = max(seekto, block+1)
		}

		if seekto <= seek {
			if closer, ok := rdr.(io.Closer); ok {
				go closer.Close()
			}
			rdr = nil
			seek = 0
		}
		worker = make(chan int64)
		go func(a, b int64) {
			var err error
			if rdr == nil {
				rdr, err = OpenFunc(id)
				if err != nil {
					rdr = nil
				}
			}
			for block := a; block < b; block++ {
				var data []byte
				if rdr != nil {
					data = make([]byte, blocksize)
					var n int
					n, err = io.ReadFull(rdr, data) // not really checking for errors here tbh
					data = data[:n]
					if err == io.ErrUnexpectedEOF {
						err = io.EOF
					}
					if err != nil {
						rdr = nil
					}
				}
				ch2 <- o2msg{
					key:   id,
					which: block,
					block: smoosh(data),
					err:   err,
				}
				worker <- block
			}
			close(worker)
		}(seek, seekto)
	}
}

func smoosh(buf []byte) []byte {
	if len(buf) == 0 {
		return nil
	} else if len(buf) <= cap(buf)/2 {
		return append(make([]byte, 0, len(buf)), buf...)
	} else {
		return buf
	}
}

func setBlockList(m map[int64]struct{}, first int64, n int64) {
	for i := first; i < first+n; i++ {
		m[i] = struct{}{}
	}
}

func clearBlockList(m map[int64]struct{}, first int64, n int64) {
	for i := first; i < first+n; i++ {
		delete(m, i)
	}
}

func init() {
	go multiplexer()
}
