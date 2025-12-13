//go:build !unix

package fileid

import (
	"io/fs"
)

func Get(fsys fs.FS, pathname string) (ID, error) {
	return ID{}, ErrNotOS
}
