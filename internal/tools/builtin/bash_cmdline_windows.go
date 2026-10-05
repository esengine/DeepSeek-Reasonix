//go:build windows

package builtin

import (
	"syscall"
	"unicode/utf16"
)

// windowsCommandLineMax is CreateProcessW's lpCommandLine ceiling in UTF-16
// code units, the terminating NUL included.
const windowsCommandLineMax = 32767

// commandLineUnits counts the UTF-16 units os/exec hands CreateProcessW for
// argv, NUL included, using the same per-argument escaping it applies.
func commandLineUnits(argv []string) (int, int) {
	units := 1
	for i, arg := range argv {
		if i > 0 {
			units++
		}
		units += len(utf16.Encode([]rune(syscall.EscapeArg(arg))))
	}
	return units, windowsCommandLineMax
}
