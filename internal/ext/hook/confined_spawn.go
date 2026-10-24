package hook

import (
	"context"
	"maps"
	"os/exec"

	"reasonix/internal/base/proc"
	"reasonix/internal/safety/sandbox"
)

func confineHookCommand(ctx context.Context, cmd *exec.Cmd, options RuntimeOptions) (*exec.Cmd, error) {
	if options.Sandbox == nil {
		return cmd, nil
	}
	if err := sandbox.RequiredIntegrityFault(*options.Sandbox); err != nil {
		return nil, err
	}
	prepared := sandbox.PrepareArgs(*options.Sandbox, cmd.Args, "")
	if prepared.Err != nil {
		return nil, prepared.Err
	}
	if !prepared.Wrapped {
		return nil, sandbox.ErrIncompleteIntegrity
	}
	return exec.CommandContext(ctx, prepared.Argv[0], prepared.Argv[1:]...), nil
}

func prepareHookRuntime(options RuntimeOptions, environment map[string]string) (RuntimeOptions, func(), error) {
	if options.Sandbox != nil {
		if err := sandbox.RequiredIntegrityFault(*options.Sandbox); err != nil {
			return options, func() {}, err
		}
	}
	if options.Sandbox == nil || options.SessionTemp == nil {
		return options, func() {}, nil
	}
	lease, err := options.SessionTemp.Acquire()
	if err != nil {
		return options, func() {}, err
	}
	spec := *options.Sandbox
	spec.SessionTemp = lease.Dir()
	spec.ShellEnv = maps.Clone(spec.ShellEnv)
	if spec.ShellEnv == nil {
		spec.ShellEnv = map[string]string{}
	}
	maps.Copy(spec.ShellEnv, environment)
	options.Sandbox = &spec
	return options, lease.Release, nil
}

func runHookProcess(ctx context.Context, cmd *exec.Cmd, options RuntimeOptions) error {
	if options.Sandbox == nil {
		return cmd.Run()
	}
	tracked, err := proc.RunCommand(ctx, cmd, proc.RunOptions{Track: true, Source: "hook"})
	if tracked != nil {
		if done := tracked.WaitCompleted(); done != nil {
			<-done
		}
		tracked.Kill()
	}
	return err
}
