//go:build !windows

package main

import "os"

// replaceLocalSaveDestination atomically replaces the destination on POSIX
// filesystems. Rename replaces a symlink itself, so a target swapped after the
// identity check cannot redirect the write into the source file.
func replaceLocalSaveDestination(temp, target string) error {
	return os.Rename(temp, target)
}
