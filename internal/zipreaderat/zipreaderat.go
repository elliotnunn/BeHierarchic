package zipreaderat

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/elliotnunn/resourceform/internal/reader2readerat"
)

type keeptrack struct {
	refcnt uintptr
	ra     *reader2readerat.Reader
}

type Archive struct {
	*zip.Reader
	reuse map[string]keeptrack
	lock  sync.Mutex
}

type File struct {
	ra   *reader2readerat.Reader
	arch *Archive
	name string
	seek int64
	stat fs.FileInfo
}

// If opening a file, guaranteed to satisfy io.ReaderAt and io.SeekReader
func (r *Archive) Open(name string) (fs.File, error) {
	f, err := r.Reader.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close() // odd, I know, but bear with me...

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("unable to stat an open zip file: %w", err)
	}
	if stat.IsDir() {
		return f, err
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	if r.reuse == nil {
		r.reuse = make(map[string]keeptrack)
	}

	saved, ok := r.reuse[name]
	if !ok {
		reopener := func() (io.Reader, error) {
			return r.Reader.Open(name)
		}
		saved = keeptrack{ra: reader2readerat.NewFromReader(reopener)}
	}
	saved.refcnt++
	r.reuse[name] = saved

	return &File{
		arch: r,
		name: name,
		ra:   saved.ra,
		seek: 0,
		stat: stat,
	}, nil
}

func (f *File) ReadAt(buf []byte, off int64) (n int, err error) {
	return f.ra.ReadAt(buf, off)
}

func (f *File) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.seek)
	f.seek += int64(n)
	return n, err
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.seek
	case io.SeekEnd:
		offset += f.stat.Size()
	default:
		return 0, errWhence
	}
	if offset < 0 {
		return 0, errOffset
	}
	f.seek = offset
	return offset, nil
}

func (f *File) Stat() (fs.FileInfo, error) {
	return f.stat, nil
}

func (f *File) Size() int64 {
	return f.stat.Size()
}

func (f *File) Close() error {
	var err error
	f.arch.lock.Lock()
	defer f.arch.lock.Unlock()
	saved := f.arch.reuse[f.name]
	saved.refcnt--
	if saved.refcnt == 0 {
		err = saved.ra.Close()
		delete(f.arch.reuse, f.name)
	} else {
		f.arch.reuse[f.name] = saved
	}
	return err
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")
