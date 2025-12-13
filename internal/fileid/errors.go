package fileid

import "errors"

// if this is returned then the stat object is okay
var ErrNotOS = errors.New("%w: filesystem is not actually the OS")
