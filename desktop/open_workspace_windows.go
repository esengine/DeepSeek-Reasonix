//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func openWorkspacePath(path string) error {
	verb, err := windows.UTF16PtrFromString(shellOpenVerb(path))
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// shellOpenVerb returns the ShellExecute verb used to open path. The "open"
// verb resolves folders through the shell namespace, where a sibling
// "<folder>.lnk" with the same base name wins and launches the shortcut's
// target instead of Explorer (#7851). The "explore" verb opens a folder in
// Explorer directly and never consults .lnk files; files keep "open".
func shellOpenVerb(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "explore"
	}
	return "open"
}
