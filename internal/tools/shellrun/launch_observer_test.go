package shellrun

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"reasonix/internal/base/proc"
	"reasonix/internal/contract/tool"
)

type launchProbe struct {
	launch    tool.ShellLaunch
	completed bool
	finishes  int
	refuse    bool
}

func (p *launchProbe) Begin(_ context.Context, launch tool.ShellLaunch) (func(bool), error) {
	p.launch = launch
	if p.refuse {
		return nil, errors.New("observation refused")
	}
	return func(completed bool) { p.finishes++; p.completed = completed }, nil
}

func TestLaunchObserverSeesFinalCommandAndCanRefuseBeforeSpawn(t *testing.T) {
	argv, shell := shellArgv(t, "echo ok")
	for _, refuse := range []bool{false, true} {
		probe := &launchProbe{refuse: refuse}
		ctx := tool.WithShellLaunchObserver(t.Context(), probe)
		called := false
		root := t.TempDir()
		result := RunForeground(ctx, Request{Argv: argv, Dir: root, Env: []string{"ACTUAL=launch-environment"}, ShellPath: shell.Path,
			Run: func(ctx context.Context, cmd *exec.Cmd, opts proc.RunOptions) (*proc.TrackedCommand, error) {
				called = true
				if probe.launch.Executable != cmd.Path || !reflect.DeepEqual(probe.launch.Arguments, cmd.Args) ||
					!reflect.DeepEqual(probe.launch.Environment, cmd.Environ()) || probe.launch.Directory != cmd.Dir {
					t.Fatal("observer did not see the actual process description")
				}
				return proc.RunCommand(ctx, cmd, opts)
			}})
		if refuse {
			if called || result.Started || result.Err == nil || probe.finishes != 0 {
				t.Fatalf("refused observation still executed: %+v", result)
			}
		} else if result.Err != nil || !called || !probe.completed || probe.finishes != 1 {
			t.Fatalf("launch completion lost: %+v, %+v", result, probe)
		}
	}
}

func TestWithoutLaunchObserverShadowsInheritedObserver(t *testing.T) {
	probe := &launchProbe{refuse: true}
	ctx := tool.WithoutShellLaunchObserver(tool.WithShellLaunchObserver(t.Context(), probe))
	finish, err := tool.ObserveShellLaunch(ctx, tool.ShellLaunch{})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if probe.finishes != 0 || probe.launch.Executable != "" {
		t.Fatal("inherited observer was invoked")
	}
}
