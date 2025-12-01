// Copyright Elliot Nunn. Portions copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import (
	"encoding/binary"
	"time"
)

// msDosTimeToTime converts an MS-DOS date and time into a time.Time.
// The resolution is 2s.
// See: https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-dosdatetimetofiletime
func msDosTimeToTime(dosDate, dosTime uint16) time.Time {
	return time.Date(
		// date bits 0-4: day of month; 5-8: month; 9-15: years since 1980
		int(dosDate>>9+1980),
		time.Month(dosDate>>5&0xf),
		int(dosDate&0x1f),

		// time bits 0-4: second/2; 5-10: minute; 11-15: hour
		int(dosTime>>11),
		int(dosTime>>5&0x3f),
		int(dosTime&0x1f*2),
		0, // nanoseconds

		time.UTC,
	)
}

func timeFromExtraField(kind int, fieldBuf []byte) time.Time {
	switch kind {
	case 10: // NTFS Extra Field
		if len(fieldBuf) < 4 {
			return time.Time{}
		}

		// interesting nesting
		subfields := parseExtra(fieldBuf[4:])
		if times, ok := subfields[1]; ok && len(times) >= 8 {
			const ticksPerSecond = 1e7                     // Windows timestamp resolution
			ts := int64(binary.LittleEndian.Uint64(times)) // ModTime since Windows epoch
			secs := ts / ticksPerSecond
			nsecs := (1e9 / ticksPerSecond) * (ts % ticksPerSecond)
			epoch := time.Date(1601, time.January, 1, 0, 0, 0, 0, time.UTC)
			return time.Unix(epoch.Unix()+secs, nsecs)
		}
	case 13, 0x5855: // Unix Extra Field, Info-Zip UNIX
		if len(fieldBuf) < 8 {
			return time.Time{}
		}
		return time.Unix(int64(binary.LittleEndian.Uint32(fieldBuf[4:])), 0)
	case 0x5455: // extended timestamp
		if len(fieldBuf) < 5 || fieldBuf[0]&1 == 0 {
			return time.Time{}
		}
		return time.Unix(int64(binary.LittleEndian.Uint32(fieldBuf[1:])), 0)
	}
	return time.Time{}
}
