//go:build windows

package storage

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// readVolume asks Windows for the free and total bytes available to this user
// on the volume holding dir. A quota-limited account gets its own allowance
// rather than the disk's, which is the number that decides whether a move fits.
func readVolume(dir string) Volume {
	vol := Volume{Path: filepath.VolumeName(dir)}
	if vol.Path == "" {
		vol.Path = dir
	}
	pathp, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return vol
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathp, &free, &total, &totalFree); err != nil {
		return vol
	}
	vol.Free = int64(free)
	vol.Total = int64(total)
	return vol
}
