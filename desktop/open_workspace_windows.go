//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func openWorkspacePath(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	err = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	return nil
}
