package tool

import (
	"context"
	"os/exec"
)

// ControlledShellRequest is host-owned execution policy, never tool arguments.
type ControlledShellRequest struct {
	Command             string
	PipeStatusMarker    string
	Directory           string
	Interpreter         string
	ReadDenies          []string
	DeclaredEnvironment map[string]string
}

// ControlledShell owns a single host verification launch. Finish inspects its
// actual command; callers cannot declare successful environmental coverage.
type ControlledShell interface {
	Prepare(context.Context, ControlledShellRequest) ([]string, error)
	Command(context.Context, ShellLaunch) (*exec.Cmd, func(), error)
	Started(*exec.Cmd)
	Cancel() error
	WaitScope() error
}

type controlledShellKey struct{}

func WithControlledShell(ctx context.Context, launcher ControlledShell) context.Context {
	return context.WithValue(ctx, controlledShellKey{}, struct{ Launcher ControlledShell }{launcher})
}

func ControlledShellFrom(ctx context.Context) ControlledShell {
	value, _ := ctx.Value(controlledShellKey{}).(struct{ Launcher ControlledShell })
	return value.Launcher
}
