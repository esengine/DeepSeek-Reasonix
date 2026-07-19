//go:build !windows

package hook

import (
	"context"
	"os/exec"
)

func windowsBatchCommand(context.Context, string) (*exec.Cmd, bool) {
	return nil, false
}

func windowsBatchArgvCommand(context.Context, string, []string) (*exec.Cmd, bool) {
	return nil, false
}
