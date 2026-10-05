//go:build unix

package pluginpkg

import "syscall"

// A nonregular source must not block the metadata check, even if it becomes a
// named pipe before the root opens it.
const agentReadFlags = syscall.O_NONBLOCK
