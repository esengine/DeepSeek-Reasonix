package main

import "fmt"

func installerCommandLine(installer, dir string) string {
	// Auto-updates use a dedicated visible progress mode instead of NSIS /S.
	// The current helper also requires staging-only extraction: NSIS never writes
	// the live install, and the helper compare-and-publishes the claimed release
	// unit after validating every staged member.
	line := fmt.Sprintf(`"%s" /REASONIXUPDATE=1 /REASONIXSTAGE=1`, installer)
	if dir != "" {
		line += " /D=" + dir
	}
	return line
}
