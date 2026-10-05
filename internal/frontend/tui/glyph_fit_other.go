//go:build !windows

package tui

import "os"

func newConsoleGlyphFit(*os.File) *glyphFit { return nil }
