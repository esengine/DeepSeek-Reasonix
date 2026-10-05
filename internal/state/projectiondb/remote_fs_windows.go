//go:build windows

package projectiondb

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func filesystemRemote(path string) bool {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" {
		return false
	}
	root := volume
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	rootPtr, err := windows.UTF16PtrFromString(root)
	return err == nil && windows.GetDriveType(rootPtr) == windows.DRIVE_REMOTE
}
