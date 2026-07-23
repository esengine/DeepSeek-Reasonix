package main

import "fmt"

func installerCommandLine(installer, dir string) string {
	// Auto-updates use a dedicated visible progress mode instead of NSIS /S.
	// The installer skips its welcome and directory pages in this mode, keeps
	// the current install directory fixed, and closes itself after the file copy
	// completes so this helper can relaunch Reasonix.
	line := fmt.Sprintf(`"%s" /REASONIXUPDATE=1`, installer)
	if dir != "" {
		line += " /D=" + dir
	}
	return line
}
