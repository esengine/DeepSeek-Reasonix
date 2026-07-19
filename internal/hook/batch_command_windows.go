//go:build windows

package hook

import (
	"context"
	"os/exec"
	"syscall"
)

func windowsBatchCommand(ctx context.Context, command string) (*exec.Cmd, bool) {
	commandLine, ok := windowsBatchCommandLine(command)
	return newWindowsBatchCommand(ctx, commandLine, ok)
}

func windowsBatchArgvCommand(ctx context.Context, command string, args []string) (*exec.Cmd, bool) {
	commandLine, ok := windowsBatchArgvCommandLine(command, args)
	return newWindowsBatchCommand(ctx, commandLine, ok)
}

func newWindowsBatchCommand(ctx context.Context, commandLine string, ok bool) (*exec.Cmd, bool) {
	if !ok {
		return nil, false
	}
	cmd := exec.CommandContext(ctx, "cmd.exe")
	// cmd.exe does not use CommandLineToArgvW. Supply the exact command line so
	// Go does not backslash-escape the leading quoted batch path.
	cmd.Args = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: commandLine}
	return cmd, true
}
