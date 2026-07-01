package builtin

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"reasonix/internal/sandbox"
)

func TestBashForegroundTimeoutConfig(t *testing.T) {
	sh := sandbox.ResolveShell("", "", nil)
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
	sh := sandbox.ResolveShell("", "", nil)
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
	sh := sandbox.ResolveShell("", "", nil)
	b := byName(Workspace{Dir: t.TempDir(), BashTimeout: 150 * time.Millisecond}.Tools())["bash"]

	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": longSleepCommand(sh)}))
	if err == nil {
		t.Fatalf("expected workspace bash timeout, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestNormalizeBashRunErrorAllowsPreservedWaitDelay(t *testing.T) {
	if err := normalizeBashRunError(context.Background(), exec.ErrWaitDelay, true); err != nil {
		t.Fatalf("preserved post-exit WaitDelay should be ignored, got %v", err)
	}
	if err := normalizeBashRunError(context.Background(), exec.ErrWaitDelay, false); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("ordinary WaitDelay should remain visible, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := normalizeBashRunError(ctx, exec.ErrWaitDelay, true); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("cancelled WaitDelay should remain visible, got %v", err)
	}
}

func TestWaitForTrackedShellProcessReturnsWhenWaitStallsAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tracked := &trackedShellProcess{}
	releaseWait := make(chan struct{})
	defer close(releaseWait)

	start := time.Now()
	err := waitForTrackedShellProcess(ctx, tracked, func() error {
		<-releaseWait
		return nil
	}, 20*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("cancelled stalled wait returned too slowly: %v", elapsed)
	}
	if !tracked.killed {
		t.Fatal("tracked process was not marked killed")
	}
}

func TestWaitForTrackedShellProcessKeepsWaitErrorAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tracked := &trackedShellProcess{}
	waitStarted := make(chan struct{})
	releaseWait := make(chan struct{})
	errWait := errors.New("wait failed after cancel")
	done := make(chan error, 1)

	go func() {
		done <- waitForTrackedShellProcess(ctx, tracked, func() error {
			close(waitStarted)
			<-releaseWait
			return errWait
		}, time.Second)
	}()

	<-waitStarted
	cancel()
	waitUntilTrackedShellKilled(t, tracked)
	close(releaseWait)

	select {
	case err := <-done:
		if err.Error() != context.Canceled.Error() {
			t.Fatalf("error text = %q, want %q", err.Error(), context.Canceled.Error())
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if !errors.Is(err, errWait) {
			t.Fatalf("error = %v, want wrapped wait error %v", err, errWait)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled shell wait")
	}
}

func waitUntilTrackedShellKilled(t *testing.T, tracked *trackedShellProcess) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		tracked.mu.Lock()
		killed := tracked.killed
		tracked.mu.Unlock()
		if killed {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for tracked shell kill")
		default:
			time.Sleep(time.Millisecond)
		}
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
