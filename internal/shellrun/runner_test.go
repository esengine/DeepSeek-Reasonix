package shellrun

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func TestDescriptorFromShell(t *testing.T) {
	tests := []struct {
		name        string
		sh          sandbox.Shell
		wantShell   string
		wantVersion string
		wantAndAnd  bool
	}{
		{
			name:       "posix bash",
			sh:         sandbox.Shell{Kind: sandbox.ShellBash, Path: "/bin/bash"},
			wantShell:  tool.ShellNameBash,
			wantAndAnd: true,
		},
		{
			name:       "git bash path",
			sh:         sandbox.Shell{Kind: sandbox.ShellBash, Path: `C:\Program Files\Git\bin\bash.exe`},
			wantShell:  tool.ShellNameGitBash,
			wantAndAnd: true,
		},
		{
			name:        "windows powershell 5.1",
			sh:          sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
			wantShell:   tool.ShellNamePowerShell,
			wantVersion: tool.ShellVersionPS51,
			wantAndAnd:  false,
		},
		{
			name:        "pwsh 7+",
			sh:          sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\Program Files\PowerShell\7\pwsh.exe`},
			wantShell:   tool.ShellNamePwsh,
			wantVersion: tool.ShellVersionPS7,
			wantAndAnd:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescriptorFromShell(tt.sh)
			if got.Shell != tt.wantShell {
				t.Fatalf("Shell = %q, want %q", got.Shell, tt.wantShell)
			}
			if got.ShellVersion != tt.wantVersion {
				t.Fatalf("ShellVersion = %q, want %q", got.ShellVersion, tt.wantVersion)
			}
			if got.SupportsAndAnd != tt.wantAndAnd {
				t.Fatalf("SupportsAndAnd = %v, want %v", got.SupportsAndAnd, tt.wantAndAnd)
			}
			if got.Kind != "shell" {
				t.Fatalf("Kind = %q", got.Kind)
			}
			if got.Platform == "" {
				t.Fatal("Platform empty")
			}
		})
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName(DescriptorFromShell(sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"})); got != "Windows PowerShell" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName(DescriptorFromShell(sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "pwsh"})); got != "PowerShell 7+" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayName(DescriptorFromShell(sandbox.Shell{Kind: sandbox.ShellBash, Path: `C:\Program Files\Git\bin\bash.exe`})); got != "Git Bash" {
		t.Fatalf("got %q", got)
	}
}

func TestRunForegroundSuccess(t *testing.T) {
	argv, sh := shellArgv(t, "printf 'ok\\n'")
	res := RunForeground(context.Background(), Request{
		Argv:      argv,
		ShellKind: sh.Kind.String(),
		ShellPath: sh.Path,
		Track:     true,
	})
	if res.Err != nil {
		t.Fatalf("err = %v", res.Err)
	}
	if res.State != tool.ShellStateCompleted {
		t.Fatalf("state = %q", res.State)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("exitCode = %v", res.ExitCode)
	}
	if !strings.Contains(res.Combined, "ok") {
		t.Fatalf("combined = %q", res.Combined)
	}
}

func TestRunForegroundNonZeroExit(t *testing.T) {
	argv, sh := shellArgv(t, "exit 7")
	res := RunForeground(context.Background(), Request{
		Argv:      argv,
		ShellKind: sh.Kind.String(),
		ShellPath: sh.Path,
		Track:     true,
	})
	if res.Err == nil {
		t.Fatal("expected error")
	}
	if res.State != tool.ShellStateFailed || res.FailurePhase != tool.ShellPhaseExecution {
		t.Fatalf("state/phase = %s/%s", res.State, res.FailurePhase)
	}
	if res.ExitCode == nil || *res.ExitCode == 0 {
		t.Fatalf("exitCode = %v", res.ExitCode)
	}
}

func TestRunForegroundTimeout(t *testing.T) {
	cmd := "sleep 5"
	sh := sandbox.ResolveShell("auto", "", nil)
	if sh.Kind == sandbox.ShellPowerShell {
		cmd = "Start-Sleep -Seconds 5"
	}
	argv, _ := shellArgv(t, cmd)
	res := RunForeground(context.Background(), Request{
		Argv:      argv,
		Timeout:   200 * time.Millisecond,
		ShellKind: sh.Kind.String(),
		ShellPath: sh.Path,
		Track:     true,
	})
	if res.State != tool.ShellStateTimedOut || res.FailurePhase != tool.ShellPhaseTimeout {
		t.Fatalf("state/phase = %s/%s err=%v", res.State, res.FailurePhase, res.Err)
	}
}

func TestRunForegroundLaunchFailure(t *testing.T) {
	res := RunForeground(context.Background(), Request{
		Argv:  []string{"/nonexistent/reasonix-shell-binary-xyz", "-c", "echo hi"},
		Track: false,
		Run: func(ctx context.Context, cmd *exec.Cmd, opts proc.RunOptions) (*proc.TrackedCommand, error) {
			return nil, errors.New("exec: no such file")
		},
	})
	if res.State != tool.ShellStateFailed || res.FailurePhase != tool.ShellPhaseLaunch {
		t.Fatalf("state/phase = %s/%s", res.State, res.FailurePhase)
	}
	if res.ExitCode != nil {
		t.Fatalf("exitCode should be nil for launch failure, got %v", *res.ExitCode)
	}
}

func TestRunForegroundOutputTailBounded(t *testing.T) {
	payload := strings.Repeat("中文", 3000)
	// Keep the command under typical argv length limits.
	if len(payload) > 4000 {
		payload = payload[:4000]
	}
	sh := sandbox.ResolveShell("auto", "", nil)
	var command string
	if sh.Kind == sandbox.ShellPowerShell {
		command = `[Console]::Error.Write('` + strings.ReplaceAll(payload, "'", "''") + `')`
	} else {
		command = "printf '%s' '" + strings.ReplaceAll(payload, "'", `'\"'\"'`) + "' 1>&2"
	}
	argv := shellArgvWith(sh, command)
	res := RunForeground(context.Background(), Request{
		Argv:      argv,
		ShellKind: sh.Kind.String(),
		ShellPath: sh.Path,
		Track:     true,
	})
	if len(res.OutputTail) > tool.OutputTailMaxBytes {
		t.Fatalf("output tail %d > %d", len(res.OutputTail), tool.OutputTailMaxBytes)
	}
	if !strings.Contains(res.Combined, "中文") && !strings.Contains(res.OutputTail, "中文") {
		t.Fatalf("UTF-8 Chinese lost: combined=%q tail=%q", trim(res.Combined, 80), trim(res.OutputTail, 80))
	}
}

// TestRunForegroundSharesOnePipeForStdoutAndStderr pins the mechanism behind
// ordered combined output: os/exec reuses a single pipe and a single copy
// goroutine only while Stdout and Stderr hold the same writer value. Giving them
// two writers (for example to tee stderr into its own tail) silently splits the
// child's streams into two pipes, and the model then reads reordered output.
func TestRunForegroundSharesOnePipeForStdoutAndStderr(t *testing.T) {
	var captured *exec.Cmd
	RunForeground(context.Background(), Request{
		Argv:     []string{"irrelevant"},
		Progress: func(string) {},
		Run: func(_ context.Context, cmd *exec.Cmd, _ proc.RunOptions) (*proc.TrackedCommand, error) {
			captured = cmd
			return nil, nil
		},
	})
	if captured == nil {
		t.Fatal("runner never built a command")
	}
	if captured.Stdout == nil || captured.Stdout != captured.Stderr {
		t.Fatalf("Stdout and Stderr must be the same writer value; got %p and %p", captured.Stdout, captured.Stderr)
	}
}

// TestRunForegroundPreservesInterleaving is the behavioral half of the same
// contract: what the child wrote first must still come first.
func TestRunForegroundPreservesInterleaving(t *testing.T) {
	sh := sandbox.ResolveShell("auto", "", nil)
	if sh.Kind == sandbox.ShellPowerShell {
		t.Skip("stream-buffering semantics differ on PowerShell; the pipe-identity test covers the mechanism")
	}
	const rounds = 8
	var want strings.Builder
	for i := 1; i <= rounds; i++ {
		fmt.Fprintf(&want, "out%d\nerr%d\n", i, i)
	}
	argv := shellArgvWith(sh, "for i in 1 2 3 4 5 6 7 8; do echo out$i; echo err$i 1>&2; done")
	// Repeat: two pipes reorder probabilistically, so one run can pass by luck.
	for run := range 10 {
		res := RunForeground(context.Background(), Request{Argv: argv, Timeout: 30 * time.Second})
		if res.Combined != want.String() {
			t.Fatalf("run %d lost child write order:\ngot  %q\nwant %q", run, res.Combined, want.String())
		}
	}
}

// TestRunForegroundDropsTailOnSuccess keeps a successful command from carrying
// up to 16 KiB of ordinary stdout into the session record and the tool card.
func TestRunForegroundDropsTailOnSuccess(t *testing.T) {
	argv, _ := shellArgv(t, "echo hello")
	res := RunForeground(context.Background(), Request{Argv: argv, Timeout: 30 * time.Second})
	if res.State != tool.ShellStateCompleted {
		t.Fatalf("State = %q, want %q", res.State, tool.ShellStateCompleted)
	}
	if !strings.Contains(res.Combined, "hello") {
		t.Fatalf("Combined = %q, want it to contain the output", res.Combined)
	}
	if res.OutputTail != "" {
		t.Fatalf("OutputTail = %q, want empty on success", res.OutputTail)
	}
}

func shellArgv(t *testing.T, command string) ([]string, sandbox.Shell) {
	t.Helper()
	sh := sandbox.ResolveShell("auto", "", nil)
	return shellArgvWith(sh, command), sh
}

func shellArgvWith(sh sandbox.Shell, command string) []string {
	path := sh.Path
	if path == "" {
		path = sh.Kind.String()
	}
	if sh.Kind == sandbox.ShellPowerShell {
		return []string{path, "-NoProfile", "-NonInteractive", "-Command", sandbox.PowerShellUTF8Script(command)}
	}
	return []string{path, "-c", command}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
