package proc

import (
	"context"
	"os/exec"
)

// Command constructs a desktop background command. On Windows it is hidden by
// default so new Git/probe/index/update checks cannot accidentally flash a
// console window.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	HideWindow(cmd)
	return cmd
}

// CommandContext is Command with cancellation.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	// Dynamic executables are intentional for approved tools and terminals; name
	// and args remain separate argv values, with any shell explicit at the caller.
	// codeql[go/command-injection]
	cmd := exec.CommandContext(ctx, name, args...)
	HideWindow(cmd)
	return cmd
}

// VisibleCommand is the explicit escape hatch for a user-requested GUI,
// terminal, installer or application relaunch.
func VisibleCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// VisibleCommandContext is the context-aware visible escape hatch.
func VisibleCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
