package zipreaderat

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"strings"

	"github.com/elliotnunn/resourceform/internal/flate"
)

type Archive struct {
	*zip.Reader
	fileList map[string]specialf
}

type specialf struct {
	reader io.ReaderAt
	stat   fs.FileInfo
}

func New(r io.ReaderAt, size int64) (*Archive, error) {
	wrapee, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}

	ret := &Archive{
		Reader:   wrapee, // passthru the zip.Reader methods
		fileList: make(map[string]specialf),
	}

	for _, f := range wrapee.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}

		data, _ := f.DataOffset()
		smSize := int64(f.CompressedSize64)
		lgSize := int64(f.UncompressedSize64)

		var reader io.ReaderAt
		switch f.Method {
		case zip.Store:
			reader = io.NewSectionReader(r, data, smSize)
		case zip.Deflate:
			reader = flate.NewReader(io.NewSectionReader(r, data, smSize), smSize, lgSize)
		}
		if reader != nil {
			ret.fileList[f.Name] = specialf{reader: reader, stat: f.FileInfo()}
		}
	}

	return ret, nil
}

func (r *Archive) Open(name string) (fs.File, error) {
	if special, ok := r.fileList[name]; ok {
		return &File{ReaderAt: special.reader, stat: special.stat}, nil
	}

	return r.Reader.Open(name)
}

type File struct {
	io.ReaderAt
	stat       fs.FileInfo
	size, seek int64
}

func (f *File) Size() int64 {
	return f.size
}

func (f *File) Stat() (fs.FileInfo, error) {
	return f.stat, nil
}

func (f *File) Close() error {
	return nil
}

func (r *File) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.seek)
	r.seek += int64(n)
	return n, err
}

func (r *File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.seek
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, errWhence
	}
	if offset < 0 {
		return 0, errOffset
	}
	r.seek = offset
	return offset, nil
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")
