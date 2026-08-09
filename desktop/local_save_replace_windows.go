//go:build windows

package main

import "golang.org/x/sys/windows"

// windows.Rename uses MoveFileEx(MOVEFILE_REPLACE_EXISTING), preserving the
// old destination when replacement fails instead of deleting it first.
func replaceLocalSaveDestination(temp, target string) error {
	return windows.Rename(temp, target)
}
