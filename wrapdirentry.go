package main

import "io/fs"

// In the world of the io/fs package, when listing a directory with ReadDir,
// you get a slice of DirEntry.
// This wrapper struct is our way of making these look like directories rather than files.

type dirEntryThatLooksLikeAFolder struct {
	fs.DirEntry
}

func (e dirEntryThatLooksLikeAFolder) IsDir() bool {
	return true
}

func (e dirEntryThatLooksLikeAFolder) Type() fs.FileMode {
	return fs.ModeDir
}
