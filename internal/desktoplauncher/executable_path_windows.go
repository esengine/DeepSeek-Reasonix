//go:build windows

package desktoplauncher

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const maxFinalPathUTF16 = 1 << 16

// resolveExecutablePath opens the launcher and asks Windows for the final DOS
// path represented by that handle. Unlike filepath.EvalSymlinks, this resolves
// directory junctions such as Scoop's stable current entry.
func resolveExecutablePath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open executable: %w", err)
	}
	defer file.Close()

	handle := windows.Handle(file.Fd())
	size := uint32(256)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], size, 0)
		if err != nil {
			return "", fmt.Errorf("get final executable path: %w", err)
		}
		if n < size {
			return normalizeFinalWindowsPath(windows.UTF16ToString(buf[:n])), nil
		}
		if n >= maxFinalPathUTF16 {
			return "", fmt.Errorf("get final executable path: required buffer is too large: %d", n)
		}
		size = n + 1
	}
}

func normalizeFinalWindowsPath(path string) string {
	const (
		extendedPrefix = `\\?\`
		extendedUNC    = `\\?\UNC\`
	)
	if len(path) >= len(extendedUNC) && strings.EqualFold(path[:len(extendedUNC)], extendedUNC) {
		return `\\` + path[len(extendedUNC):]
	}
	if len(path) >= 7 && strings.EqualFold(path[:len(extendedPrefix)], extendedPrefix) &&
		isASCIILetter(path[4]) && path[5] == ':' && path[6] == '\\' {
		return path[len(extendedPrefix):]
	}
	return path
}

func isASCIILetter(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}
