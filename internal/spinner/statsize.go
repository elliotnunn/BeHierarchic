// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package spinner

import (
	"errors"
)

var errSizeUnknown = errors.New("file size not known ahead of time (e.g. streamed gzip)")

func sizeOf(id Opener) (int64, error) {
	f, err := id.Open()
	if err != nil {
		return 0, err
	}
	defer f.Close()
	type sizer interface{ Size() int64 }
	if sizer, ok := f.(sizer); ok {
		return sizer.Size(), nil
	}
	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := stat.Size()
	if size < 0 {
		return 0, errSizeUnknown
	}
	return size, nil
}
