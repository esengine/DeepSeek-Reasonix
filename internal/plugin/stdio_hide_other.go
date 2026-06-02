//go:build !windows

package plugin

import "os/exec"

// setPlatformAttrs is a no-op on non-Windows platforms. macOS and Linux do not
// have the concept of a "console window" for subprocesses — child stderr flows
// to the parent's stderr naturally.
func setPlatformAttrs(cmd *exec.Cmd) {}
