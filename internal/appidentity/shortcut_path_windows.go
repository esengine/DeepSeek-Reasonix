//go:build windows

package appidentity

import (
	"os"

	"golang.org/x/sys/windows"

	"reasonix/internal/fileutil"
)

func existingShortcutPath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return fileutil.FinalWindowsPath(windows.Handle(file.Fd()))
}
