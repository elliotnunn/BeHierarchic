package resourcefork

import (
	"io"
	"io/fs"
	"time"
)

type typeDir struct {
	fsys       *FS
	t          [4]byte
	nOfType    uint32
	typeOffset uint32
	listOffset int
}

func (*typeDir) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

func (d *typeDir) ReadDir(count int) ([]fs.DirEntry, error) {
	n := int(d.nOfType) - d.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && n > count {
		n = count
	}

	list := make([]fs.DirEntry, n)
	d.fsys.listResources(list, d.typeOffset+uint32(12*d.listOffset))
	d.listOffset += n
	return list, nil
}

func (d *typeDir) Stat() (fs.FileInfo, error) {
	return d, nil
}

func (*typeDir) Close() error {
	return nil
}

func (s *typeDir) Name() string { // FileInfo + DirEntry
	return stringFromType(s.t)
}

func (*typeDir) IsDir() bool { // FileInfo + DirEntry
	return true
}

func (*typeDir) Type() fs.FileMode { // DirEntry
	return fs.ModeDir
}

func (s *typeDir) Info() (fs.FileInfo, error) { // DirEntry
	return s, nil
}

func (*typeDir) Size() int64 { // FileInfo
	return 0
}

func (*typeDir) Mode() fs.FileMode { // FileInfo
	return 0o777
}

func (d *typeDir) ModTime() time.Time { // FileInfo
	return d.fsys.mtime()
}

func (s *typeDir) Sys() any { // FileInfo
	return nil
}
