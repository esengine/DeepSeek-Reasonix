//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func openWorkspacePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return openWorkspacePathWithType(path, info.IsDir())
}

func openWorkspacePathWithType(path string, isDir bool) error {
	verb, target := shellOpenCommand(path, isDir)
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
// Folders use "explore" and a trailing separator so the shell cannot confuse
// them with a sibling "<folder>.lnk" and launch its target (#7851). Files keep
// the "open" verb and their original path.
func shellOpenCommand(path string, isDir bool) (verb, target string) {
	if isDir {
		target = path
		if target == "" || !os.IsPathSeparator(target[len(target)-1]) {
			target += string(os.PathSeparator)
		}
		return "explore", target
	}
	return "open", path
}
