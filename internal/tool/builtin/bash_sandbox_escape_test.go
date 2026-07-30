package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/sandbox"
)

type fakeSandboxEscapeApprover struct {
	allow          bool
	reason         string
	sessionAllowed bool
	calls          []sandbox.EscapeRequest
	sessionChecks  []sandbox.EscapeRequest
}

func (f *fakeSandboxEscapeApprover) ApproveSandboxEscape(ctx context.Context, req sandbox.EscapeRequest) (bool, string, error) {
	f.calls = append(f.calls, req)
	return f.allow, f.reason, nil
}

func (f *fakeSandboxEscapeApprover) SandboxEscapeSessionAllowed(ctx context.Context, req sandbox.EscapeRequest) bool {
	f.sessionChecks = append(f.sessionChecks, req)
	return f.sessionAllowed
}

func TestBashSandboxUnavailableCanEscapeOnceWithApproval(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()

	sh := sandbox.ResolveShell("", "", nil)
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()

	approver := &fakeSandboxEscapeApprover{allow: true}
	ctx := sandbox.WithEscapeApprover(context.Background(), approver)
	args := argsJSON(t, map[string]any{"command": echoForShell(sh, "escaped")})
	out, err := (bash{sb: sandbox.Spec{Mode: "enforce"}, shell: sh}).Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute returned error after approved escape: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "escaped") {
		t.Fatalf("output = %q, want escaped command output", out)
	}
	if len(approver.calls) != 1 {
		t.Fatalf("approval calls = %d, want 1", len(approver.calls))
	}
	if approver.calls[0].Command != echoForShell(sh, "escaped") {
		t.Fatalf("approval command = %q", approver.calls[0].Command)
	}
	if !json.Valid(approver.calls[0].Args) {
		t.Fatalf("approval args are not valid JSON: %q", approver.calls[0].Args)
	}
}

func TestBashSandboxUnavailableStaysClosedWithoutApprover(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()

	sh := sandbox.ResolveShell("", "", nil)
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()

	out, err := (bash{sb: sandbox.Spec{Mode: "enforce"}, shell: sh}).Execute(context.Background(), argsJSON(t, map[string]any{"command": echoForShell(sh, "escaped")}))
	if err == nil {
		t.Fatalf("Execute succeeded without escape approver, out=%q", out)
	}
	if !strings.Contains(err.Error(), "sandbox requested but unavailable") {
		t.Fatalf("error = %v, want unavailable sandbox message", err)
	}
}

func TestBashSandboxEscapeDenialBlocksUnconfinedRun(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()

	sh := sandbox.ResolveShell("", "", nil)
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()

	approver := &fakeSandboxEscapeApprover{allow: false, reason: "declined escape"}
	ctx := sandbox.WithEscapeApprover(context.Background(), approver)
	out, err := (bash{sb: sandbox.Spec{Mode: "enforce"}, shell: sh}).Execute(ctx, argsJSON(t, map[string]any{"command": echoForShell(sh, "escaped")}))
	if err == nil {
		t.Fatalf("Execute succeeded after denied escape, out=%q", out)
	}
	if !strings.Contains(err.Error(), "declined escape") {
		t.Fatalf("error = %v, want denial reason", err)
	}
	if len(approver.calls) != 1 {
		t.Fatalf("approval calls = %d, want 1", len(approver.calls))
	}
}

func TestBashSandboxEscapeSessionGrantRunsForegroundUnconfinedBeforeWrapper(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()

	sh := sandbox.ResolveShell("", "", nil)
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		if spec.Enforce() {
			return unconfinedShellArgv(sh, windowsSandboxFailureForShell(sh)), true
		}
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()
	approver := &fakeSandboxEscapeApprover{sessionAllowed: true}
	ctx := sandbox.WithEscapeApprover(context.Background(), approver)
	out, err := (bash{sb: sandbox.Spec{Mode: "enforce"}, shell: sh}).Execute(ctx, argsJSON(t, map[string]any{"command": echoForShell(sh, "session-rerun")}))
	if err != nil {
		t.Fatalf("Execute returned error with session escape: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "session-rerun") {
		t.Fatalf("output = %q, want unconfined command output", out)
	}
	if strings.Contains(out, "windows sandbox: boom") {
		t.Fatalf("output should not come from sandbox helper, got %q", out)
	}
	if len(approver.calls) != 0 {
		t.Fatalf("fresh approval calls = %d, want 0", len(approver.calls))
	}
	if len(approver.sessionChecks) != 1 {
		t.Fatalf("session checks = %d, want 1", len(approver.sessionChecks))
	}
}

func TestBashSandboxEscapeSessionGrantRunsBackgroundUnconfinedBeforeWrapper(t *testing.T) {
	restore := forceWindowsSandboxEscapeTestMode(t)
	defer restore()

	sh := sandbox.ResolveShell("", "", nil)
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		if spec.Enforce() {
			return unconfinedShellArgv(sh, windowsSandboxFailureForShell(sh)), true
		}
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()

	approver := &fakeSandboxEscapeApprover{sessionAllowed: true}
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := jobs.WithManager(context.Background(), jm)
	ctx = jobs.WithSession(ctx, "session-a")
	ctx = sandbox.WithEscapeApprover(ctx, approver)

	out, err := (bash{sb: sandbox.Spec{Mode: "enforce"}, shell: sh}).Execute(ctx, argsJSON(t, map[string]any{
		"command":           echoForShell(sh, "background-real"),
		"run_in_background": true,
	}))
	if err != nil {
		t.Fatalf("Execute returned error starting background job with session escape: %v (out=%q)", err, out)
	}
	jobID := backgroundJobIDFromStartOutput(t, out)
	results := jm.WaitForSession(context.Background(), "session-a", []string{jobID}, 5)
	if len(results) != 1 {
		t.Fatalf("wait results = %d, want 1", len(results))
	}
	if results[0].Status != jobs.Done {
		t.Fatalf("job status = %s, want %s (output=%q)", results[0].Status, jobs.Done, results[0].Output)
	}
	if !strings.Contains(results[0].Output, "background-real") {
		t.Fatalf("job output = %q, want unconfined command output", results[0].Output)
	}
	if strings.Contains(results[0].Output, "windows sandbox: boom") {
		t.Fatalf("job output should not come from sandbox helper, got %q", results[0].Output)
	}
	if len(approver.calls) != 0 {
		t.Fatalf("fresh approval calls = %d, want 0", len(approver.calls))
	}
	if len(approver.sessionChecks) != 1 {
		t.Fatalf("session checks = %d, want 1", len(approver.sessionChecks))
	}
}

func forceWindowsSandboxEscapeTestMode(t *testing.T) func() {
	t.Helper()
	old := bashSandboxEscapePromptEnabled
	bashSandboxEscapePromptEnabled = func() bool { return true }
	return func() { bashSandboxEscapePromptEnabled = old }
}

func echoForShell(sh sandbox.Shell, text string) string {
	if sh.Kind == sandbox.ShellPowerShell {
		return "Write-Output " + text
	}
	return "printf " + text
}

func windowsSandboxFailureForShell(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellPowerShell {
		return "Write-Error 'windows sandbox: boom'; exit 126"
	}
	return "printf 'windows sandbox: boom\\n' >&2; exit 126"
}

func backgroundJobIDFromStartOutput(t *testing.T, out string) string {
	t.Helper()
	const prefix = `Started background job "`
	start := strings.Index(out, prefix)
	if start < 0 {
		t.Fatalf("start output = %q, want background job id", out)
	}
	rest := out[start+len(prefix):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("start output = %q, want closing quote for background job id", out)
	}
	return rest[:end]
}
