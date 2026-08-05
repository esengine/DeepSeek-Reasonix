//go:build windows

package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	maxFinalConfigPathUTF16   = 1 << 16
	finalConfigVolumeNameDOS  = uint32(0x0)
	finalConfigVolumeNameGUID = uint32(0x1)
)

type finalConfigPathQuery func(flags uint32) (string, error)

// resolveExistingConfigPath opens an existing path and asks Windows for the
// final path represented by that handle. filepath.EvalSymlinks can fail on
// otherwise accessible paths that cross a directory junction into a Cloud
// Files provider such as OneDrive.
func resolveExistingConfigPath(path string) (string, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("encode config path: %w", err)
	}
	handle, err := windows.CreateFile(
		pathp,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("open config path: %w", err)
	}
	defer windows.CloseHandle(handle)

	resolved, err := resolveFinalConfigWindowsPath(func(flags uint32) (string, error) {
		return queryFinalConfigWindowsPath(handle, flags)
	})
	if err != nil {
		return "", fmt.Errorf("get final config path: %w", err)
	}
	return resolved, nil
}

func resolveFinalConfigWindowsPath(query finalConfigPathQuery) (string, error) {
	resolved, err := query(finalConfigVolumeNameDOS)
	if errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		guidResolved, guidErr := query(finalConfigVolumeNameGUID)
		if guidErr != nil {
			return "", fmt.Errorf("DOS path unavailable and volume GUID fallback failed: %w", errors.Join(err, guidErr))
		}
		resolved, err = guidResolved, nil
	}
	if err != nil {
		return "", err
	}
	if resolved == "" || !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("Windows returned non-absolute path %q", resolved)
	}
	// Keep the extended namespace prefix returned by Windows. Removing it can
	// change the meaning of names that contain trailing spaces or dots.
	return resolved, nil
}

func queryFinalConfigWindowsPath(handle windows.Handle, flags uint32) (string, error) {
	size := uint32(256)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], size, flags)
		if err != nil {
			return "", err
		}
		if n < size {
			return windows.UTF16ToString(buf[:n]), nil
		}
		if n >= maxFinalConfigPathUTF16 {
			return "", fmt.Errorf("required buffer is too large: %d", n)
		}
		size = n + 1
	}
}

// configPathLockKey removes only equivalent DOS/UNC namespace prefixes for
// hashing. The actual I/O path keeps its extended prefix and exact semantics.
func configPathLockKey(path string) string {
	const (
		extendedPrefix = `\\?\`
		extendedUNC    = `\\?\UNC\`
	)
	if len(path) >= len(extendedUNC) && strings.EqualFold(path[:len(extendedUNC)], extendedUNC) {
		return `\\` + path[len(extendedUNC):]
	}
	if len(path) >= 7 && strings.EqualFold(path[:len(extendedPrefix)], extendedPrefix) &&
		isASCIIConfigPathLetter(path[4]) && path[5] == ':' && path[6] == '\\' {
		return path[len(extendedPrefix):]
	}
	return path
}

func isASCIIConfigPathLetter(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}
