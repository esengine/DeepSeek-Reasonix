//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// ansiConsoleReady reports whether stdout can render SGR sequences, enabling
// VT processing when the console has it off (legacy conhost prints the escapes
// literally instead — see #3242). A non-console stdout is left to colorprofile.
func ansiConsoleReady() bool {
	enableVirtualTerminal(os.Stderr)
	return enableVirtualTerminal(os.Stdout)
}

func enableVirtualTerminal(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return true
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
