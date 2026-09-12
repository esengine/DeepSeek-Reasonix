package builtin

import (
	"context"
	"strings"
	"testing"

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

func TestBashSandboxUnavailableFailsClosedEvenWithLegacyApprover(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "sandbox requested but unavailable") {
		t.Fatalf("Execute = (%q, %v), want fail-closed sandbox error", out, err)
	}
	if len(approver.calls) != 0 {
		t.Fatalf("legacy escape approver was called %d times", len(approver.calls))
	}
}

func TestBashSandboxUnavailableStaysClosedWithoutApprover(t *testing.T) {
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

func TestBashSandboxUnavailableDoesNotOpenLegacyDenialPrompt(t *testing.T) {
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
	if !strings.Contains(err.Error(), "sandbox requested but unavailable") {
		t.Fatalf("error = %v, want fail-closed sandbox reason", err)
	}
	if len(approver.calls) != 0 {
		t.Fatalf("legacy escape approver was called %d times", len(approver.calls))
	}
}

func TestBashLegacySessionEscapeCannotBypassForegroundSandbox(t *testing.T) {
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
	if err == nil || !strings.Contains(out, "windows sandbox: boom") {
		t.Fatalf("Execute = (%q, %v), want enforced sandbox failure", out, err)
	}
	if len(approver.calls) != 0 {
		t.Fatalf("fresh approval calls = %d, want 0", len(approver.calls))
	}
	if len(approver.sessionChecks) != 0 {
		t.Fatalf("legacy session grant was consulted %d times", len(approver.sessionChecks))
	}
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
