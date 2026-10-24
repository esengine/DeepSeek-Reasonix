package shellrun

import (
	"context"
	"os/exec"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/contract/tool"
)

type launchObservation struct {
	ordinary func(bool)
	physical func()
}

func runForegroundProcess(ctx context.Context, req Request, cmd *exec.Cmd, waitDelay time.Duration) (*proc.TrackedCommand, error) {
	run := req.Run
	if run == nil {
		run = proc.RunCommand
	}
	opts := proc.RunOptions{Track: req.Track, CancelWaitGrace: waitDelay + time.Second,
		Source: foregroundSource(req), ShellKind: req.ShellKind, ShellPath: req.ShellPath, CommandPreview: req.CommandPreview}
	if owner := tool.ControlledShellFrom(ctx); owner != nil {
		opts.OnStarted, opts.CancelScope, opts.WaitScope = owner.Started, owner.Cancel, owner.WaitScope
	}
	return run(ctx, cmd, opts)
}

func foregroundSource(req Request) string {
	if req.Source == "" {
		return "shellrun"
	}
	return req.Source
}

func (o launchObservation) end(completed bool, wait <-chan struct{}) {
	if o.physical != nil {
		proc.AfterWait(wait, o.physical)
	} else if o.ordinary != nil {
		o.ordinary(completed)
	}
}

func prepareForegroundCommand(ctx context.Context, req Request, waitDelay time.Duration) (*exec.Cmd, launchObservation, error) {
	if launcher := tool.ControlledShellFrom(ctx); launcher != nil {
		cmd, finish, err := launcher.Command(ctx, tool.ShellLaunch{Executable: req.Argv[0], Interpreter: req.ShellPath, Directory: req.Dir,
			Arguments: append([]string(nil), req.Argv...), Environment: append([]string(nil), req.Env...), Command: req.Command})
		if err != nil {
			return nil, launchObservation{}, err
		}
		cmd.WaitDelay = waitDelay
		return cmd, launchObservation{physical: finish}, nil
	}
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = req.Dir, req.Env, waitDelay
	finish, err := tool.ObserveShellLaunch(ctx, tool.ShellLaunch{Executable: cmd.Path, Interpreter: req.ShellPath, Directory: cmd.Dir,
		Arguments: append([]string(nil), cmd.Args...), Environment: cmd.Environ(), Command: req.Command})
	return cmd, launchObservation{ordinary: finish}, err
}
