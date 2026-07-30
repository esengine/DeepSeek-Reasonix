package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/sandbox"
)

func TestResolvedProcessModeDefaultsToHost(t *testing.T) {
	if got := (Spec{}).ResolvedProcessMode(); got != MCPProcessHost {
		t.Fatalf("empty ProcessMode = %q, want host", got)
	}
	if got := (Spec{ProcessMode: MCPProcessConfined}).ResolvedProcessMode(); got != MCPProcessConfined {
		t.Fatalf("confined ProcessMode = %q", got)
	}
}

func TestHostModeEnsureConnectedSkipsCommandSandboxAndSharesProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startCount := filepath.Join(t.TempDir(), "starts")
	stateDir := t.TempDir()
	spec := Spec{
		Name:        "hostmode",
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess", "--"},
		ProcessMode: MCPProcessHost,
		StateDir:    stateDir,
		// Host mode must ignore an enforce sandbox; private StateDir still applies.
		Sandbox: sandbox.Spec{
			Mode:          "enforce",
			MinimalWrites: true,
			WriteRoots:    []string{stateDir},
		},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":     "1",
			"GO_WANT_HELPER_START_COUNT": startCount,
			"GO_WANT_HELPER_INIT_MS":     "150",
		},
	}

	host := NewHost()
	defer host.Close()

	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 3)
	for i := 0; i < 3; i++ {
		go func() {
			tools, err := host.EnsureConnected(ctx, spec)
			ch <- result{len(tools), err}
		}()
	}
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("EnsureConnected: %v", r.err)
		}
		if r.n != 2 {
			t.Fatalf("tools = %d, want 2", r.n)
		}
	}
	if got := readHelperCounter(t, startCount); got != 1 {
		t.Fatalf("stdio process starts = %d, want 1", got)
	}
	// Private state directory is created even in host mode.
	if _, err := os.Stat(filepath.Join(stateDir, "cache")); err != nil {
		t.Fatalf("host mode should still create private cache dir: %v", err)
	}
}

func TestEnsureConnectedCancelWaitDoesNotKillSharedProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startCount := filepath.Join(t.TempDir(), "starts")
	spec := Spec{
		Name:        "shared-cancel",
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess", "--"},
		ProcessMode: MCPProcessHost,
		StateDir:    t.TempDir(),
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":     "1",
			"GO_WANT_HELPER_START_COUNT": startCount,
			"GO_WANT_HELPER_INIT_MS":     "400",
		},
	}
	host := NewHost()
	defer host.Close()

	// Start the long-lived owner first so it claims the single-flight spawn.
	// A later short-timeout waiter must only cancel its wait, not the shared process.
	ownerStarted := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		close(ownerStarted)
		_, err := host.EnsureConnected(ctx, spec)
		ownerDone <- err
	}()
	<-ownerStarted
	// Give the owner time to claim beginSpawn before the waiter races it.
	time.Sleep(30 * time.Millisecond)

	waitCtx, waitCancel := context.WithTimeout(ctx, 40*time.Millisecond)
	_, waitErr := host.EnsureConnected(waitCtx, spec)
	waitCancel()
	if waitErr == nil {
		t.Fatal("cancelled waiter should return an error")
	}

	if err := <-ownerDone; err != nil {
		t.Fatalf("owner EnsureConnected should still succeed after waiter cancel: %v", err)
	}
	if got := readHelperCounter(t, startCount); got != 1 {
		t.Fatalf("shared process starts = %d, want 1", got)
	}
	if !host.HasClient("shared-cancel") {
		t.Fatal("shared client should remain after waiter cancel")
	}
}
