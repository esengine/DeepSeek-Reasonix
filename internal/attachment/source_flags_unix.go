//go:build unix

package attachment

import (
	"os"
	"syscall"
)

// A file replaced by a FIFO between Lstat and Open must not stall admission.
const imageReadFlags = os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
