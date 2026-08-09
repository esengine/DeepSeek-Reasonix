//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func openWorkspacePath(path string) error {
	verb, target := shellOpenCommand(path)
	verbPtr, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	filePtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verbPtr, filePtr, nil, nil, windows.SW_SHOWNORMAL)
}

// shellOpenCommand returns the ShellExecute verb and target used to open path.
// Folders use the "explore" verb with a trailing separator: both force the
// shell to treat the target as a directory, so a sibling "<folder>.lnk" with
// the same base name can never hijack the open and launch the shortcut's
// target instead of Explorer (#7851). Files keep the "open" verb and the path
// unchanged.
func shellOpenCommand(path string) (verb, target string) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "explore", path + string(os.PathSeparator)
	}
	return "open", path
}
