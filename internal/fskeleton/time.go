// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"math"
	"time"
)

var (
	earliest = time.Unix(0, math.MinInt64).UTC()
	latest   = time.Unix(0, math.MaxInt64).UTC()
)

// This package internally stores times as int64 nsec from the Unix epoch, UTC
// These functions ensure that the zero value of stdlib [time.Time] round-trips

func timeFromStdlib(t time.Time) int64 {
	switch {
	case t.Before(earliest): // includes the zero time
		return math.MinInt64
	case t.After(latest):
		return math.MaxInt64
	default:
		return t.UnixNano()
	}
}

func timeToStdlib(t int64) time.Time {
	switch t {
	case math.MinInt64:
		return time.Time{}
	case math.MaxInt64:
		return latest
	default:
		return time.Unix(0, t).UTC()
	}
}
