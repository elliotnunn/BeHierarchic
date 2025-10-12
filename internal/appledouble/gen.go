package appledouble

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"path"
	"time"
)

// Everything that we would want to put in an AppleDouble file, except for a resource fork, because that's big.
// Excludes some of the Finder info fields that become meaningless when moving to a different disk
type AppleDouble struct {
	// Basic filesystem metadata
	CreateTime, ModTime, BkTime, AccTime time.Time
	Locked                               bool

	// Stored in various ways across software versions, nonetheless important
	Comment string

	// File-and-directory FinderInfo
	Flags    uint16
	Location struct{ Y, X int16 }
	XFlags   uint16 // ignore the rarely used "filename display script" function

	// File-only FinderInfo
	Type    [4]byte
	Creator [4]byte

	// Directory-only FinderInfo
	Rect   struct{ T, L, B, R int16 }
	View   int16 // 0 is not a valid value, use 256 (icon view)
	Scroll struct{ Y, X int16 }

	// These Finder info fields were excluded as meaningless outside the original volume:
	//   Fldr IconID Reserved Comment PutAway OpenChain Script
	// The Script field was meant to declare the text encoding of the file *name*,
	// but was oft corrupted, abandoned in System 7 and overloaded with XFlags.
}

func MacTime(t uint32) time.Time { return macEpoch.Add(time.Second * time.Duration(t)) }

func Sidecar(name string) string {
	a, b := path.Split(name)
	return a + "._" + b
}

const (
	FlagIsOnDesk            = 0x0001 // Files and folders (System 6)
	MaskColor               = 0x000E // Files and folders
	FlagRequireSwitchLaunch = 0x0020 // Applications only
	FlagIsShared            = 0x0040 // Applications only
	FlagHasNoINITs          = 0x0080 // Extensions/Control Panels only
	FlagHasBeenInited       = 0x0100 // Files only (all BNDL/FREF/open/kind have been added)
	FlagAOCELetter          = 0x0200 // obsoleted
	FlagHasCustomIcon       = 0x0400 // Files and folders
	FlagIsStationery        = 0x0800 // Files only
	FlagNameLocked          = 0x1000 // Files and folders
	FlagHasBundle           = 0x2000 // Files only
	FlagIsInvisible         = 0x4000 // Files and folders
	FlagIsAlias             = 0x8000 // Files only
	XFlagHasCustomBadge     = 0x0100
	XFlagHasRoutingInfo     = 0x0004
)

func (m *AppleDouble) LoadFInfo(d *[16]byte) {
	copy(m.Type[:], d[:])
	copy(m.Creator[:], d[4:])
	m.Flags = binary.BigEndian.Uint16(d[8:])
	m.Location.Y = int16(binary.BigEndian.Uint16(d[10:]))
	m.Location.X = int16(binary.BigEndian.Uint16(d[12:]))
}

func (m *AppleDouble) LoadFXInfo(d *[16]byte) {
	m.XFlags = binary.BigEndian.Uint16(d[8:])
	if m.XFlags&0x8000 != 0 {
		m.XFlags = 0 // the disagreeable rarely-used "filename script" field
	}
}

func (m *AppleDouble) LoadDInfo(d *[16]byte) {
	m.Rect.T = int16(binary.BigEndian.Uint16(d[:]))
	m.Rect.L = int16(binary.BigEndian.Uint16(d[2:]))
	m.Rect.B = int16(binary.BigEndian.Uint16(d[4:]))
	m.Rect.R = int16(binary.BigEndian.Uint16(d[6:]))
	m.Flags = binary.BigEndian.Uint16(d[8:])
	m.Location.Y = int16(binary.BigEndian.Uint16(d[10:]))
	m.Location.X = int16(binary.BigEndian.Uint16(d[12:]))
	m.View = int16(binary.BigEndian.Uint16(d[14:]))
}

func (m *AppleDouble) LoadDXInfo(d *[16]byte) {
	m.Scroll.Y = int16(binary.BigEndian.Uint16(d[:]))
	m.Scroll.X = int16(binary.BigEndian.Uint16(d[2:]))
	m.XFlags = binary.BigEndian.Uint16(d[8:])
	if m.XFlags&0x8000 != 0 {
		m.XFlags = 0 // the disagreeable rarely-used "filename script" field
	}
}

func (m *AppleDouble) dirInfoRec() [32]byte {
	var d [32]byte
	binary.BigEndian.PutUint16(d[:], uint16(m.Rect.T))
	binary.BigEndian.PutUint16(d[2:], uint16(m.Rect.L))
	binary.BigEndian.PutUint16(d[4:], uint16(m.Rect.B))
	binary.BigEndian.PutUint16(d[6:], uint16(m.Rect.R))
	binary.BigEndian.PutUint16(d[8:], m.Flags)
	binary.BigEndian.PutUint16(d[10:], uint16(m.Location.Y))
	binary.BigEndian.PutUint16(d[12:], uint16(m.Location.X))
	binary.BigEndian.PutUint16(d[14:], uint16(m.View))
	binary.BigEndian.PutUint16(d[16:], uint16(m.Scroll.X))
	binary.BigEndian.PutUint16(d[16+2:], uint16(m.Scroll.Y))
	binary.BigEndian.PutUint16(d[16+8:], m.XFlags)
	return d
}

func (m *AppleDouble) fileInfoRec() [32]byte {
	var d [32]byte
	copy(d[:], m.Type[:])
	copy(d[4:], m.Creator[:])
	binary.BigEndian.PutUint16(d[8:], m.Flags)
	binary.BigEndian.PutUint16(d[10:], uint16(m.Location.Y))
	binary.BigEndian.PutUint16(d[12:], uint16(m.Location.X))
	binary.BigEndian.PutUint16(d[16+8:], m.XFlags)
	return d
}

func (m *AppleDouble) datesRec() [16]byte {
	var d [16]byte
	for i, t := range []time.Time{m.CreateTime, m.ModTime, m.BkTime, m.AccTime} {
		stamp := t.Sub(appleDoubleEpoch)
		stamp = min(math.MaxInt32, stamp)
		stamp = max(math.MinInt32, stamp)
		binary.BigEndian.PutUint32(d[4*i:], uint32(stamp))
	}
	return d
}

func (m *AppleDouble) flagsRec() [4]byte {
	if m.Locked {
		return [4]byte{0x80, 0, 0, 0}
	} else {
		return [4]byte{0x0, 0, 0, 0}
	}
}

// These methods return reader interfaces: leaves scope to compress the data in future

func (m *AppleDouble) ForDir() (func() io.Reader, int64) {
	finder, dates, flags := m.dirInfoRec(), m.datesRec(), m.flagsRec()
	recs := map[int][]byte{
		FINDER_INFO:         finder[:],
		FILE_DATES_INFO:     dates[:],
		MACINTOSH_FILE_INFO: flags[:]}
	ad, _ := MakePrefix(recs, 0, 0)
	return func() io.Reader { return bytes.NewReader(ad) }, int64(len(ad))
}

func (m *AppleDouble) WithResourceFork(r io.ReaderAt, size int64) (io.ReaderAt, int64) {
	finder, dates, flags := m.fileInfoRec(), m.datesRec(), m.flagsRec()
	recs := map[int][]byte{
		FINDER_INFO:         finder[:],
		FILE_DATES_INFO:     dates[:],
		MACINTOSH_FILE_INFO: flags[:]}
	ad, rfStart := MakePrefix(recs, size, 0) // no need to pad wasteful

	if size == 0 {
		return bytes.NewReader(ad), int64(len(ad))
	}

	return &readerAt{ad: ad, fork: r}, rfStart + size
}

func (m *AppleDouble) WithSequentialResourceFork(opener func() io.Reader, size int64) (func() io.Reader, int64) {
	finder, dates, flags := m.fileInfoRec(), m.datesRec(), m.flagsRec()
	recs := map[int][]byte{
		FINDER_INFO:         finder[:],
		FILE_DATES_INFO:     dates[:],
		MACINTOSH_FILE_INFO: flags[:]}
	ad, rfStart := MakePrefix(recs, size, 8192) // pad for performance reasons

	if size == 0 {
		return func() io.Reader { return bytes.NewReader(ad) }, int64(len(ad))
	}

	return func() io.Reader {
		return &reader{ad: ad, zero: int(rfStart) - len(ad), opener: opener}
	}, rfStart + size
}
