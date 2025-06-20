package webdavadapter

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

const debug = true

type FileSystem struct {
	Inner fs.FS
}

// The three create/update/delete calls are stubbed out

func (*FileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if debug {
		log.Printf("Mkdir(%q, %s)", name, perm)
	}
	return fs.ErrPermission
}

func (*FileSystem) RemoveAll(ctx context.Context, name string) error {
	if debug {
		log.Printf("RemoveAll(%q, %s)", name)
	}
	return fs.ErrPermission
}

func (*FileSystem) Rename(ctx context.Context, oldName, newName string) error {
	if debug {
		log.Printf("Rename(%q, %q)", oldName, newName)
	}
	return fs.ErrPermission
}

func (fsys *FileSystem) OpenFile(_ context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if debug {
		log.Printf("OpenFile(%q, %#x, %s)", name, flag, perm)
	}
	f, err := fsys.Inner.Open(pathCvt(name))
	if errors.Is(err, fs.ErrInvalid) {
		return nil, fs.ErrNotExist
	} else if err != nil {
		return nil, err
	}
	return &File{Inner: f}, nil
}

func (fsys *FileSystem) Stat(_ context.Context, name string) (os.FileInfo, error) {
	if debug {
		log.Printf("Stat(%q)", name)
	}
	s, err := fs.Stat(fsys.Inner, pathCvt(name))
	if errors.Is(err, fs.ErrInvalid) {
		err = fs.ErrNotExist
	}
	return s, err
}

// [FileSystem.OpenFile] is guaranteed to return [*File]
type File struct {
	Inner fs.File
	seek  int64
}

func (f *File) Close() error {
	if debug {
		log.Print("File.Close()")
	}
	return f.Inner.Close()
}

func (f *File) Read(p []byte) (n int, err error) {
	if debug {
		log.Printf("File.Read(%d)", len(p))
	}
	n, err = f.Inner.Read(p)
	f.seek += int64(n)
	return n, err
}

func (f *File) Readdir(count int) ([]fs.FileInfo, error) {
	if debug {
		log.Printf("File.Readdir(%d)", count)
	}
	if rdf, ok := f.Inner.(fs.ReadDirFile); ok {
		dirEntrySlice, err := rdf.ReadDir(count)
		fileInfoSlice := make([]fs.FileInfo, 0, len(dirEntrySlice))
		for _, de := range dirEntrySlice {
			fileInfoSlice = append(fileInfoSlice, &FileInfo{Inner: de})
		}
		return fileInfoSlice, err
	} else {
		return nil, io.EOF
	}
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	if debug {
		log.Printf("File.Seek(%d, %d)", offset, whence)
	}
	if f, ok := f.Inner.(io.Seeker); ok {
		return f.Seek(offset, whence)
	} else {
		panic("these should all seek")
	}
}

func (f *File) Stat() (fs.FileInfo, error) {
	if debug {
		log.Print("File.Stat()")
	}
	return f.Inner.Stat()
}

func (f *File) Write(p []byte) (n int, err error) {
	if debug {
		log.Printf("File.Write(%d)", len(p))
	}
	return 0, fs.ErrPermission
}

type FileInfo struct {
	Inner  fs.DirEntry
	once   sync.Once
	inner2 fs.FileInfo
}

func (i *FileInfo) expensive() {
	i.once.Do(func() {
		i.inner2, _ = i.Inner.Info()
	})
}

func (i *FileInfo) Name() string {
	if debug {
		log.Print("FileInfo.Name()")
	}
	return i.Inner.Name()
}

func (i *FileInfo) Size() int64 {
	if debug {
		log.Print("FileInfo.Size()")
	}
	i.expensive()
	if i.inner2 == nil {
		return 0
	}
	return i.inner2.Size()
}

func (i *FileInfo) Mode() fs.FileMode {
	if debug {
		log.Print("FileInfo.Mode()")
	}
	if i.Inner.Type() == fs.ModeDir {
		return fs.ModeDir | 0o777
	} else {
		return fs.ModeDir | 0o666
	}
}

func (i *FileInfo) ModTime() time.Time {
	if debug {
		log.Print("FileInfo.ModTime()")
	}
	i.expensive()
	if i.inner2 == nil {
		return time.Unix(0, 0)
	}
	return i.inner2.ModTime()
}

func (i *FileInfo) IsDir() bool {
	if debug {
		log.Print("FileInfo.IsDir()")
	}
	return i.Inner.IsDir()
}

func (i *FileInfo) Sys() any {
	if debug {
		log.Print("FileInfo.Sys()")
	}
	return nil
}

func pathCvt(p string) string {
	if p == "/" {
		return "."
	} else {
		p = strings.Trim(p, "/")
		return p
	}
}
