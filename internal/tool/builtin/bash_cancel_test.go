package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// TestBashCancelReturnsPromptly proves a cancelled bash run stops fast instead of
// blocking for the command's natural duration — the process-tree kill path.
func TestBashCancelReturnsPromptly(t *testing.T) {
	bt, ok := tool.LookupBuiltin("bash")
	if !ok {
		t.Fatal("bash not registered")
	}
	cmd := "sleep 120"
	if sandbox.ResolveShell("", "", nil).Kind == sandbox.ShellPowerShell {
		cmd = "Start-Sleep -Seconds 120"
	}
	args, _ := json.Marshal(map[string]any{"command": cmd})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := bt.Execute(ctx, args)
		done <- err
	}()

	// The kill must land well before the 120s natural duration; the generous
	// watchdog only trips when the cancel path is actually broken, so a loaded
	// machine's slow process-tree teardown doesn't flake the test.
	var err error
	select {
	case err = <-done:
	case <-time.After(40 * time.Second):
		t.Fatalf("cancel did not interrupt bash within 40s (natural duration 120s)")
	}
	elapsed := time.Since(start)

	// Must have run until the cancel (≥ ~300ms) — not failed instantly.
	if elapsed < 250*time.Millisecond {
		t.Fatalf("command exited too fast (%v) — it didn't actually run; err=%v", elapsed, err)
	}
	if err == nil {
		t.Error("expected an error after cancel, got nil")
	}
	t.Logf("cancelled bash (%q) returned in %v (err=%v)", cmd, elapsed, err)
}
