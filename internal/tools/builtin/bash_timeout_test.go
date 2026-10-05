package builtin

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/sandbox"
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
	// The call set no deadline of its own, so the message has to name the
	// parameter it never reached for — otherwise the cap reads as a dead end.
	if !strings.Contains(err.Error(), "host cap") || !strings.Contains(err.Error(), "timeout_seconds") {
		t.Fatalf("error = %v, want the host cap and its per-call override named", err)
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
	b := byName(Workspace{Dir: testenv.TempDir(t), BashTimeout: 150 * time.Millisecond}.Tools())["bash"]

	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": longSleepCommand(sh)}))
	if err == nil {
		t.Fatalf("expected workspace bash timeout, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestBashForegroundTimeoutResolution(t *testing.T) {
	cases := []struct {
		name string
		host time.Duration
		sec  int
		want time.Duration
	}{
		{"omitted keeps the host cap", 120 * time.Second, 0, 120 * time.Second},
		{"per-call tightens the host cap", 120 * time.Second, 10, 10 * time.Second},
		{"per-call cannot raise the host cap", 120 * time.Second, 600, 120 * time.Second},
		{"per-call is the only deadline without a cap", 0, 10, 10 * time.Second},
		{"omitted without a cap stays unbounded", 0, 0, 0},
		{"negative cap is unbounded, not instant", -1, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (bash{timeout: c.host}).foregroundTimeout(c.sec); got != c.want {
				t.Errorf("foregroundTimeout(%d) with host cap %v = %v, want %v", c.sec, c.host, got, c.want)
			}
		})
	}
}

// The per-call deadline has to reach the process, not just the resolver: with
// the host cap disabled it is the only thing that can stop the command.
func TestBashPerCallTimeoutAppliesWithoutHostCap(t *testing.T) {
	sh := sandbox.ResolveShell("", "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	out, err := (bash{shell: sh, timeout: 0}).Execute(ctx, argsJSON(t, map[string]any{
		"command":         longSleepCommand(sh),
		"timeout_seconds": 1,
	}))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected per-call timeout, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if !strings.Contains(err.Error(), "1s") {
		t.Fatalf("error = %v, want the per-call deadline named so the model can tell whose deadline fired", err)
	}
	if !strings.Contains(err.Error(), "raise timeout_seconds") {
		t.Fatalf("error = %v, want the exit for a self-chosen deadline", err)
	}
	if strings.Contains(err.Error(), "host cap") {
		t.Fatalf("error = %v, the call chose this deadline, not the host", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("per-call timeout returned too slowly: %v", elapsed)
	}
}

// A real run asked for 600s under a 120s cap. Telling it to raise the number
// the cap already overrode would send it back to the same wall.
func TestBashTimeoutExitDoesNotAdviseRaisingACappedRequest(t *testing.T) {
	b := bash{timeout: 120 * time.Second}

	capped := b.timeoutExit(600)
	if strings.Contains(capped, "raise timeout_seconds") {
		t.Errorf("exit = %q, must not advise raising a request the cap already overrode", capped)
	}
	for _, want := range []string{"2m0s", "600", "run_in_background"} {
		if !strings.Contains(capped, want) {
			t.Errorf("exit = %q, missing %q", capped, want)
		}
	}

	if room := b.timeoutExit(10); !strings.Contains(room, "raise timeout_seconds") {
		t.Errorf("exit = %q, a request under the cap can still be raised", room)
	}
	if none := b.timeoutExit(0); !strings.Contains(none, "host cap") {
		t.Errorf("exit = %q, a call that set no deadline should hear about the cap", none)
	}
	// With the cap disabled the per-call value is the only deadline, so raising
	// it is once again the real advice.
	uncapped := (bash{timeout: 0}).timeoutExit(600)
	if !strings.Contains(uncapped, "raise timeout_seconds") {
		t.Errorf("exit = %q, nothing overrode this request", uncapped)
	}
}

func TestBashPerCallTimeoutCannotOutlastHostCap(t *testing.T) {
	sh := sandbox.ResolveShell("", "", nil)

	start := time.Now()
	out, err := (bash{shell: sh, timeout: 150 * time.Millisecond}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command":         longSleepCommand(sh),
		"timeout_seconds": 60,
	}))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected the host cap to fire, got nil (out=%q)", out)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("per-call value outlasted the host cap: %v", elapsed)
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
		timeout := bt.foregroundTimeout(0)
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
		timeout := bt.foregroundTimeout(0)
		if timeout > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeoutCause(ctx, timeout, errors.New("bash foreground timeout"))
			cancel()
		}
		if runCtx == nil {
			b.Fatal("nil context")
		}
	}
}
