//go:build linux

package builtin

import (
	"os"
	"strconv"
)

// openedPath is the path the kernel holds for an open file, which no rename or
// link swapped after a check can change.
func openedPath(f *os.File) (string, bool) {
	p, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	return p, err == nil
}
