package resourcefork

import (
	"io"
	"io/fs"
	"time"
)

type typeDir struct {
	fsys       *FS
	t          [4]byte
	nOfType    uint16
	listOffset uint16
	typeOffset uint32
}

func (*typeDir) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

func (d *typeDir) ReadDir(count int) ([]fs.DirEntry, error) {
	n := d.nOfType - d.listOffset
	if n == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && int(n) > count {
		n = uint16(count)
	}

	list, err := d.fsys.listResources(d.typeOffset+uint32(12*d.listOffset), n)
	d.listOffset += uint16(len(list))
	return list, err
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
	return d.fsys.ModTime
}

func (s *typeDir) Sys() any { // FileInfo
	return nil
}
