package shellrun

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func TestPreflightFailureBlocksUserCommandAndKeepsEvidence(t *testing.T) {
	const diagnostic = "0 [main] bash: *** fatal error - CreateFileMapping S-1-5-21-1.1, Win32 error 5. Terminating."
	calls := 0
	res := RunForeground(context.Background(), Request{
		Argv: []string{"must-not-run"}, ProbeArgv: []string{"fake-shell", t.Name()},
		Dir: t.TempDir(), Env: []string{"LANG=C", "TEMP=private"},
		Run: func(_ context.Context, cmd *exec.Cmd, _ proc.RunOptions) (*proc.TrackedCommand, error) {
			calls++
			if cmd.Args[0] != "fake-shell" || !reflect.DeepEqual(cmd.Env, []string{"LANG=C", "TEMP=private"}) {
				t.Fatalf("preflight changed command/environment: %v %v", cmd.Args, cmd.Env)
			}
			fmt.Fprint(cmd.Stderr, diagnostic)
			return nil, errors.New("exit status 256")
		},
	})
	if calls != 1 || res.Started || res.State != tool.ShellStateNotRun || res.FailurePhase != tool.ShellPhasePreflight || res.Combined != diagnostic {
		t.Fatalf("unsafe/opaque preflight failure: calls=%d result=%+v", calls, res)
	}
}

func TestPreflightFailureCacheSeparatesExecutionContexts(t *testing.T) {
	cache := &probeFailures{}
	calls := 0
	req := Request{ProbeArgv: []string{"sandbox-helper", "policy-A", "shell"}, Env: []string{"TEMP=A"}, Dir: "A"}
	req.Run = func(_ context.Context, _ *exec.Cmd, _ proc.RunOptions) (*proc.TrackedCommand, error) {
		calls++
		return nil, errors.New("startup denied")
	}
	for range 4 {
		if cache.check(context.Background(), req) == nil {
			t.Fatal("failure lost")
		}
	}
	if calls != 1 {
		t.Fatalf("repeated failed launches: %d", calls)
	}
	req.Env = []string{"TEMP=B"}
	cache.check(context.Background(), req)
	req.Dir = "B"
	cache.check(context.Background(), req)
	req.ProbeArgv = []string{"sandbox-helper", "policy-B", "shell"}
	cache.check(context.Background(), req)
	req.Timeout = 150 * time.Millisecond
	cache.check(context.Background(), req)
	if calls != 5 {
		t.Fatalf("distinct execution contexts shared failure: %d", calls)
	}
}

func TestPreflightRequiresChildReadinessBeforeUserCommand(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			calls := 0
			res := RunForeground(context.Background(), Request{
				Argv: []string{"user-command"}, ProbeArgv: []string{"probe", t.Name()}, Dir: t.TempDir(),
				Run: func(_ context.Context, cmd *exec.Cmd, _ proc.RunOptions) (*proc.TrackedCommand, error) {
					calls++
					if cmd.Args[0] == "probe" && ready {
						fmt.Fprintln(cmd.Stdout, shellProbeMarker)
					}
					return nil, nil
				},
			})
			if ready && (res.Err != nil || calls != 2) {
				t.Fatalf("healthy launch: %+v calls=%d", res, calls)
			}
			if !ready && (res.Err == nil || calls != 1) {
				t.Fatalf("missing marker accepted: %+v", res)
			}
		})
	}
}

func TestWindowsRuntimeDiagnosticsRequireEvidence(t *testing.T) {
	for _, text := range []string{"exit status 256", "access denied", "CreateFileMapping failed", "Win32 error 5"} {
		if WindowsRuntimeDiagnostic(text) != "" {
			t.Fatalf("misclassified %q", text)
		}
	}
	for _, text := range []string{
		"*** fatal error - CreateFileMapping S-1-5-21-1.1, Win32 error 5. Terminating.",
		"cygheap_user::init: NtSetInformationToken (TokenDefaultDacl), 0xC0000022",
	} {
		if WindowsRuntimeDiagnostic(text) == "" {
			t.Fatalf("missing diagnostic for %q", text)
		}
	}
}

func TestShellProbeUsesSelectedInterpreterForChild(t *testing.T) {
	for _, sh := range []sandbox.Shell{
		{Kind: sandbox.ShellBash, Path: `C:\Git\bin\bash.exe`},
		{Kind: sandbox.ShellPowerShell, Path: `C:\PowerShell\pwsh.exe`},
	} {
		command := shellProbeCommand(sh)
		if !strings.Contains(command, "exit 0") || !strings.Contains(command, shellProbeMarker) {
			t.Fatalf("probe lacks child process or readiness: %q", command)
		}
	}
}
