//go:build !windows

package sandbox

import (
	"fmt"
	"os"
)

// RunWindowsSandboxHelper is the hidden helper entry point on Windows. Other
// platforms keep a stub so the CLI can route the internal command uniformly.
func RunWindowsSandboxHelper(_ []string, _ *os.File, _ *os.File, stderr *os.File) int {
	fmt.Fprintln(stderr, "windows sandbox helper is only available on Windows")
	return 2
}
