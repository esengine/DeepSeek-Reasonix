package builtin

import (
	"context"
	"errors"
	"maps"
	"slices"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

func (b bash) prepareControlledLaunch(ctx context.Context, launcher tool.ControlledShell, sh sandbox.Shell, params bashParams) (sandbox.Prepared, pipeStatusProbe, *sessiontemp.Lease, error) {
	if sh.Kind != sandbox.ShellBash || params.RunInBackground || params.PreserveBackgroundProcesses || hasExplicitBackgroundKeepalive(params.Command) {
		return sandbox.Prepared{}, pipeStatusProbe{}, nil, errors.New("controlled verification requires a foreground Bash invocation")
	}
	probe := newPipeStatusProbe(sh, params.Command, false)
	argv, err := launcher.Prepare(ctx, tool.ControlledShellRequest{Command: params.Command, PipeStatusMarker: probe.marker(), Directory: b.workDir,
		Interpreter: sh.Path, ReadDenies: slices.Clone(b.sb.ForbidReadRoots), DeclaredEnvironment: maps.Clone(b.sb.ShellEnv)})
	return sandbox.Prepared{Argv: argv, Wrapped: true}, probe, nil, err
}
