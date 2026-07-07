package builtin

import (
	"context"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/sandbox"
)

// A foreground bash command keeps the Windows sandbox's short lock wait so a
// queued turn fails fast with the holder named; a background job blocks nobody
// while it queues, so it opts into the patient wait instead of dying while
// another sandboxed command holds the workspace.
func TestBashSandboxLockWaitForegroundVsBackground(t *testing.T) {
	sh := sandbox.ResolveShell("", "", nil)
	var mu sync.Mutex
	var waits []time.Duration
	oldCommand := bashSandboxCommand
	bashSandboxCommand = func(spec sandbox.Spec, sh sandbox.Shell, command string) ([]string, bool) {
		mu.Lock()
		waits = append(waits, spec.WindowsLockWait)
		mu.Unlock()
		return unconfinedShellArgv(sh, command), false
	}
	defer func() { bashSandboxCommand = oldCommand }()

	if _, err := (bash{shell: sh}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command": echoForShell(sh, "fg"),
	})); err != nil {
		t.Fatalf("foreground bash: %v", err)
	}

	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithManager(context.Background(), m)
	if _, err := (bash{shell: sh}).Execute(ctx, argsJSON(t, map[string]any{
		"command":           echoForShell(sh, "bg"),
		"run_in_background": true,
	})); err != nil {
		t.Fatalf("background bash: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 2 {
		t.Fatalf("sandbox wraps = %d, want 2", len(waits))
	}
	if waits[0] != 0 {
		t.Fatalf("foreground WindowsLockWait = %s, want 0 (the short interactive default)", waits[0])
	}
	if waits[1] != windowsBackgroundSandboxLockWait {
		t.Fatalf("background WindowsLockWait = %s, want %s", waits[1], windowsBackgroundSandboxLockWait)
	}
}
