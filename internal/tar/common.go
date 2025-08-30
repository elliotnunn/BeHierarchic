// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tar implements access to tar archives.
//
// It is a close copy of the standard [archive/tar] package,
// but it follows the [io/fs.FS] interface for on-line access.
// This requires the entire archive to be scanned to build a directory.
package tar

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strings"
	"time"
)

// BUG: Use of the Uid and Gid fields in Header could overflow on 32-bit
// architectures. If a large value is encountered when decoding, the result
// stored in Header will be the truncated version.

var (
	ErrHeader          = errors.New("tar: invalid tar header")
	ErrWriteTooLong    = errors.New("tar: write too long")
	ErrFieldTooLong    = errors.New("tar: header field too long")
	ErrWriteAfterClose = errors.New("tar: write after close")
	ErrInsecurePath    = errors.New("tar: insecure file path")
	errMissData        = errors.New("tar: sparse file references non-existent data")
	errUnrefData       = errors.New("tar: sparse file contains unreferenced data")
	errWriteHole       = errors.New("tar: write non-NUL byte in sparse hole")
)

type headerError []string

func (he headerError) Error() string {
	const prefix = "tar: cannot encode header"
	var ss []string
	for _, s := range he {
		if s != "" {
			ss = append(ss, s)
		}
	}
	if len(ss) == 0 {
		return prefix
	}
	return fmt.Sprintf("%s: %v", prefix, strings.Join(ss, "; and "))
}

// Type flags for Header.Typeflag.
const (
	// Type '0' indicates a regular file.
	TypeReg = '0'

	// Deprecated: Use TypeReg instead.
	TypeRegA = '\x00'

	// Type '1' to '6' are header-only flags and may not have a data body.
	TypeLink    = '1' // Hard link
	TypeSymlink = '2' // Symbolic link
	TypeChar    = '3' // Character device node
	TypeBlock   = '4' // Block device node
	TypeDir     = '5' // Directory
	TypeFifo    = '6' // FIFO node

	// Type '7' is reserved.
	TypeCont = '7'

	// Type 'x' is used by the PAX format to store key-value records that
	// are only relevant to the next file.
	// This package transparently handles these types.
	TypeXHeader = 'x'

	// Type 'g' is used by the PAX format to store key-value records that
	// are relevant to all subsequent files.
	// This package only supports parsing and composing such headers,
	// but does not currently support persisting the global state across files.
	TypeXGlobalHeader = 'g'

	// Type 'S' indicates a sparse file in the GNU format.
	TypeGNUSparse = 'S'

	// Types 'L' and 'K' are used by the GNU format for a meta file
	// used to store the path or link name for the next file.
	// This package transparently handles these types.
	TypeGNULongName = 'L'
	TypeGNULongLink = 'K'
)

// Keywords for PAX extended header records.
const (
	paxNone     = "" // Indicates that no PAX key is suitable
	paxPath     = "path"
	paxLinkpath = "linkpath"
	paxSize     = "size"
	paxUid      = "uid"
	paxGid      = "gid"
	paxUname    = "uname"
	paxGname    = "gname"
	paxMtime    = "mtime"
	paxAtime    = "atime"
	paxCtime    = "ctime"   // Removed from later revision of PAX spec, but was valid
	paxCharset  = "charset" // Currently unused
	paxComment  = "comment" // Currently unused

	paxSchilyXattr = "SCHILY.xattr."

	// Keywords for GNU sparse files in a PAX extended header.
	paxGNUSparse          = "GNU.sparse."
	paxGNUSparseNumBlocks = "GNU.sparse.numblocks"
	paxGNUSparseOffset    = "GNU.sparse.offset"
	paxGNUSparseNumBytes  = "GNU.sparse.numbytes"
	paxGNUSparseMap       = "GNU.sparse.map"
	paxGNUSparseName      = "GNU.sparse.name"
	paxGNUSparseMajor     = "GNU.sparse.major"
	paxGNUSparseMinor     = "GNU.sparse.minor"
	paxGNUSparseSize      = "GNU.sparse.size"
	paxGNUSparseRealSize  = "GNU.sparse.realsize"
)

// basicKeys is a set of the PAX keys for which we have built-in support.
// This does not contain "charset" or "comment", which are both PAX-specific,
// so adding them as first-class features of Header is unlikely.
// Users can use the PAXRecords field to set it themselves.
var basicKeys = map[string]bool{
	paxPath: true, paxLinkpath: true, paxSize: true, paxUid: true, paxGid: true,
	paxUname: true, paxGname: true, paxMtime: true, paxAtime: true, paxCtime: true,
}

// A Header represents a single header in a tar archive.
// Some fields may not be populated.
//
// For forward compatibility, users that retrieve a Header from Reader.Next,
// mutate it in some ways, and then pass it back to Writer.WriteHeader
// should do so by creating a new Header and copying the fields
// that they are interested in preserving.
type Header struct {
	// Typeflag is the type of header entry.
	// The zero value is automatically promoted to either TypeReg or TypeDir
	// depending on the presence of a trailing slash in Name.
	Typeflag byte

	Name     string // Name of file entry
	Linkname string // Target name of link (valid for TypeLink or TypeSymlink)

	Size  int64  // Logical file size in bytes
	Mode  int64  // Permission and mode bits
	Uid   int    // User ID of owner
	Gid   int    // Group ID of owner
	Uname string // User name of owner
	Gname string // Group name of owner

	// If the Format is unspecified, then Writer.WriteHeader rounds ModTime
	// to the nearest second and ignores the AccessTime and ChangeTime fields.
	//
	// To use AccessTime or ChangeTime, specify the Format as PAX or GNU.
	// To use sub-second resolution, specify the Format as PAX.
	ModTime    time.Time // Modification time
	AccessTime time.Time // Access time (requires either PAX or GNU support)
	ChangeTime time.Time // Change time (requires either PAX or GNU support)

	Devmajor int64 // Major device number (valid for TypeChar or TypeBlock)
	Devminor int64 // Minor device number (valid for TypeChar or TypeBlock)

	// Xattrs stores extended attributes as PAX records under the
	// "SCHILY.xattr." namespace.
	//
	// The following are semantically equivalent:
	//  h.Xattrs[key] = value
	//  h.PAXRecords["SCHILY.xattr."+key] = value
	//
	// When Writer.WriteHeader is called, the contents of Xattrs will take
	// precedence over those in PAXRecords.
	//
	// Deprecated: Use PAXRecords instead.
	Xattrs map[string]string

	// PAXRecords is a map of PAX extended header records.
	//
	// User-defined records should have keys of the following form:
	//	VENDOR.keyword
	// Where VENDOR is some namespace in all uppercase, and keyword may
	// not contain the '=' character (e.g., "GOLANG.pkg.version").
	// The key and value should be non-empty UTF-8 strings.
	//
	// When Writer.WriteHeader is called, PAX records derived from the
	// other fields in Header take precedence over PAXRecords.
	PAXRecords map[string]string

	// Format specifies the format of the tar header.
	//
	// This is set by Reader.Next as a best-effort guess at the format.
	// Since the Reader liberally reads some non-compliant files,
	// it is possible for this to be FormatUnknown.
	//
	// If the format is unspecified when Writer.WriteHeader is called,
	// then it uses the first format (in the order of USTAR, PAX, GNU)
	// capable of encoding this Header (see Format).
	Format Format
}

// sparseEntry represents a Length-sized fragment at Offset in the file.
type sparseEntry struct{ Offset, Length int64 }

func (s sparseEntry) endOffset() int64 { return s.Offset + s.Length }

// A sparse file can be represented as either a sparseDatas or a sparseHoles.
// As long as the total size is known, they are equivalent and one can be
// converted to the other form and back. The various tar formats with sparse
// file support represent sparse files in the sparseDatas form. That is, they
// specify the fragments in the file that has data, and treat everything else as
// having zero bytes. As such, the encoding and decoding logic in this package
// deals with sparseDatas.
//
// However, the external API uses sparseHoles instead of sparseDatas because the
// zero value of sparseHoles logically represents a normal file (i.e., there are
// no holes in it). On the other hand, the zero value of sparseDatas implies
// that the file has no data in it, which is rather odd.
//
// As an example, if the underlying raw file contains the 10-byte data:
//
//	var compactFile = "abcdefgh"
//
// And the sparse map has the following entries:
//
//	var spd sparseDatas = []sparseEntry{
//		{Offset: 2,  Length: 5},  // Data fragment for 2..6
//		{Offset: 18, Length: 3},  // Data fragment for 18..20
//	}
//	var sph sparseHoles = []sparseEntry{
//		{Offset: 0,  Length: 2},  // Hole fragment for 0..1
//		{Offset: 7,  Length: 11}, // Hole fragment for 7..17
//		{Offset: 21, Length: 4},  // Hole fragment for 21..24
//	}
//
// Then the content of the resulting sparse file with a Header.Size of 25 is:
//
//	var sparseFile = "\x00"*2 + "abcde" + "\x00"*11 + "fgh" + "\x00"*4
type (
	sparseDatas []sparseEntry
	sparseHoles []sparseEntry
)

// validateSparseEntries reports whether sp is a valid sparse map.
// It does not matter whether sp represents data fragments or hole fragments.
func validateSparseEntries(sp []sparseEntry, size int64) bool {
	// Validate all sparse entries. These are the same checks as performed by
	// the BSD tar utility.
	if size < 0 {
		return false
	}
	var pre sparseEntry
	for _, cur := range sp {
		switch {
		case cur.Offset < 0 || cur.Length < 0:
			return false // Negative values are never okay
		case cur.Offset > math.MaxInt64-cur.Length:
			return false // Integer overflow with large length
		case cur.endOffset() > size:
			return false // Region extends beyond the actual size
		case pre.endOffset() > cur.Offset:
			return false // Regions cannot overlap and must be in order
		}
		pre = cur
	}
	return true
}

// alignSparseEntries mutates src and returns dst where each fragment's
// starting offset is aligned up to the nearest block edge, and each
// ending offset is aligned down to the nearest block edge.
//
// Even though the Go tar Reader and the BSD tar utility can handle entries
// with arbitrary offsets and lengths, the GNU tar utility can only handle
// offsets and lengths that are multiples of blockSize.
func alignSparseEntries(src []sparseEntry, size int64) []sparseEntry {
	dst := src[:0]
	for _, s := range src {
		pos, end := s.Offset, s.endOffset()
		pos += blockPadding(+pos) // Round-up to nearest blockSize
		if end != size {
			end -= blockPadding(-end) // Round-down to nearest blockSize
		}
		if pos < end {
			dst = append(dst, sparseEntry{Offset: pos, Length: end - pos})
		}
	}
	return dst
}

// invertSparseEntries converts a sparse map from one form to the other.
// If the input is sparseHoles, then it will output sparseDatas and vice-versa.
// The input must have been already validated.
//
// This function mutates src and returns a normalized map where:
//   - adjacent fragments are coalesced together
//   - only the last fragment may be empty
//   - the endOffset of the last fragment is the total size
func invertSparseEntries(src []sparseEntry, size int64) []sparseEntry {
	dst := src[:0]
	var pre sparseEntry
	for _, cur := range src {
		if cur.Length == 0 {
			continue // Skip empty fragments
		}
		pre.Length = cur.Offset - pre.Offset
		if pre.Length > 0 {
			dst = append(dst, pre) // Only add non-empty fragments
		}
		pre.Offset = cur.endOffset()
	}
	pre.Length = size - pre.Offset // Possibly the only empty fragment
	return append(dst, pre)
}

// FileInfo returns an fs.FileInfo for the Header.
func (h *Header) FileInfo() fs.FileInfo {
	return headerFileInfo{h}
}

// headerFileInfo implements fs.FileInfo.
type headerFileInfo struct {
	h *Header
}

func (fi headerFileInfo) Size() int64        { return fi.h.Size }
func (fi headerFileInfo) IsDir() bool        { return fi.Mode().IsDir() }
func (fi headerFileInfo) ModTime() time.Time { return fi.h.ModTime }
func (fi headerFileInfo) Sys() any           { return fi.h }

// Name returns the base name of the file.
func (fi headerFileInfo) Name() string {
	if fi.IsDir() {
		return path.Base(path.Clean(fi.h.Name))
	}
	return path.Base(fi.h.Name)
}

// Mode returns the permission and mode bits for the headerFileInfo.
func (fi headerFileInfo) Mode() (mode fs.FileMode) {
	// Set file permission bits.
	mode = fs.FileMode(fi.h.Mode).Perm()

	// Set setuid, setgid and sticky bits.
	if fi.h.Mode&c_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if fi.h.Mode&c_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if fi.h.Mode&c_ISVTX != 0 {
		mode |= fs.ModeSticky
	}

	// Set file mode bits; clear perm, setuid, setgid, and sticky bits.
	switch m := fs.FileMode(fi.h.Mode) &^ 07777; m {
	case c_ISDIR:
		mode |= fs.ModeDir
	case c_ISFIFO:
		mode |= fs.ModeNamedPipe
	case c_ISLNK:
		mode |= fs.ModeSymlink
	case c_ISBLK:
		mode |= fs.ModeDevice
	case c_ISCHR:
		mode |= fs.ModeDevice
		mode |= fs.ModeCharDevice
	case c_ISSOCK:
		mode |= fs.ModeSocket
	}

	switch fi.h.Typeflag {
	case TypeSymlink:
		mode |= fs.ModeSymlink
	case TypeChar:
		mode |= fs.ModeDevice
		mode |= fs.ModeCharDevice
	case TypeBlock:
		mode |= fs.ModeDevice
	case TypeDir:
		mode |= fs.ModeDir
	case TypeFifo:
		mode |= fs.ModeNamedPipe
	}

	return mode
}

func (fi headerFileInfo) String() string {
	return fs.FormatFileInfo(fi)
}

const (
	// Mode constants from the USTAR spec:
	// See http://pubs.opengroup.org/onlinepubs/9699919799/utilities/pax.html#tag_20_92_13_06
	c_ISUID = 04000 // Set uid
	c_ISGID = 02000 // Set gid
	c_ISVTX = 01000 // Save text (sticky bit)

	// Common Unix mode constants; these are not defined in any common tar standard.
	// Header.FileInfo understands these, but FileInfoHeader will never produce these.
	c_ISDIR  = 040000  // Directory
	c_ISFIFO = 010000  // FIFO
	c_ISREG  = 0100000 // Regular file
	c_ISLNK  = 0120000 // Symbolic link
	c_ISBLK  = 060000  // Block special file
	c_ISCHR  = 020000  // Character special file
	c_ISSOCK = 0140000 // Socket
)

// isHeaderOnlyType checks if the given type flag is of the type that has no
// data section even if a size is specified.
func isHeaderOnlyType(flag byte) bool {
	switch flag {
	case TypeLink, TypeSymlink, TypeChar, TypeBlock, TypeDir, TypeFifo:
		return true
	default:
		return false
	}
}
