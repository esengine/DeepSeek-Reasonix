package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/sandbox"
)

func TestBashForegroundTimeoutConfig(t *testing.T) {
	sh := sandbox.ResolveShell()
	b := bash{shell: sh, timeout: 150 * time.Millisecond}

	start := time.Now()
	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": longSleepCommand(sh)}))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("configured timeout returned too slowly: %v", elapsed)
	}
}

func TestBashExplicitZeroTimeoutDoesNotCapForeground(t *testing.T) {
	sh := sandbox.ResolveShell()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	out, err := (bash{shell: sh, timeout: 0}).Execute(ctx, argsJSON(t, map[string]any{"command": oneSecondCommand(sh)}))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("zero-timeout foreground command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("output = %q, want done", out)
	}
	if elapsed < 800*time.Millisecond {
		t.Fatalf("command returned too quickly (%v), so the sleep did not run", elapsed)
	}
}

func TestWorkspacePassesBashTimeout(t *testing.T) {
	sh := sandbox.ResolveShell()
	b := byName(Workspace{Dir: t.TempDir(), BashTimeout: 150 * time.Millisecond}.Tools())["bash"]

	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": longSleepCommand(sh)}))
	if err == nil {
		t.Fatalf("expected workspace bash timeout, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func longSleepCommand(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellPowerShell {
		return "Start-Sleep -Seconds 2"
	}
	return "sleep 2"
}

func oneSecondCommand(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellPowerShell {
		return "Start-Sleep -Seconds 1; Write-Output done"
	}
	return "sleep 1; printf done"
}

func BenchmarkBashForegroundTimeoutExplicitZero(b *testing.B) {
	bt := bash{timeout: 0}
	ctx := context.Background()
	for b.Loop() {
		runCtx := ctx
		timeout := bt.foregroundTimeout()
		if timeout > 0 {
			b.Fatal("zero-value bash should not create a timeout context")
		}
		if runCtx == nil {
			b.Fatal("nil context")
		}
	}
}

func BenchmarkBashForegroundTimeoutConfiguredCap(b *testing.B) {
	bt := bash{timeout: 120 * time.Second}
	ctx := context.Background()
	for b.Loop() {
		runCtx := ctx
		timeout := bt.foregroundTimeout()
		if timeout > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeoutCause(ctx, timeout, errBashTimeout)
			cancel()
		}
		if runCtx == nil {
			b.Fatal("nil context")
		}
	}
}
