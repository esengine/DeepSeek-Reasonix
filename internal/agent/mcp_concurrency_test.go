package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestMCPServerPolicyDefaultsToParallelExceptKnownStateful(t *testing.T) {
	for name, tc := range map[string]struct {
		entry config.PluginEntry
		want  bool
	}{
		"ordinary server":         {config.PluginEntry{Name: "github"}, false},
		"filesystem server":       {config.PluginEntry{Name: "filesystem"}, false},
		"browser server":          {config.PluginEntry{Name: "browser"}, true},
		"vendor-prefixed browser": {config.PluginEntry{Name: "acme-playwright-mcp"}, true},
		"explicit serial":         {config.PluginEntry{Name: "github", Concurrency: "serial"}, true},
		"explicit parallel wins":  {config.PluginEntry{Name: "browser", Concurrency: "parallel"}, false},
	} {
		if got := mcpServerIsSerial(tc.entry); got != tc.want {
			t.Errorf("%s: serial = %v, want %v", name, got, tc.want)
		}
	}
}

func runtimeWithServers(entries ...config.PluginEntry) *MCPCapabilityRuntime {
	r := &MCPCapabilityRuntime{servers: map[string]mcpRuntimeServer{}}
	for _, e := range entries {
		r.servers[e.Name] = mcpRuntimeServer{entry: e, enabled: true}
	}
	return r
}

// Two children sharing one stdio process must not interleave on a server that
// carries session state, even though nothing it does looks like a write.
func TestMCPSerialServerNeverRunsTwoCallsAtOnce(t *testing.T) {
	r := runtimeWithServers(config.PluginEntry{Name: "browser"})
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			err := r.withServerGate(context.Background(), "browser", func() error {
				current := inFlight.Add(1)
				for {
					seen := peak.Load()
					if current <= seen || peak.CompareAndSwap(seen, current) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("withServerGate: %v", err)
			}
		})
	}
	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Fatalf("peak concurrent calls = %d, want 1: a stateful server must be serialised", got)
	}
}

// The shared-Host performance tradeoff must survive: ordinary servers keep
// running concurrently, and an unconfigured one is never gated by accident.
func TestMCPParallelServersStayConcurrent(t *testing.T) {
	for _, server := range []string{"github", "unconfigured"} {
		r := runtimeWithServers(config.PluginEntry{Name: "github"})
		held := make(chan struct{})
		released := make(chan struct{})
		go func() {
			_ = r.withServerGate(context.Background(), server, func() error {
				close(held)
				<-released
				return nil
			})
		}()
		<-held

		done := make(chan struct{})
		go func() {
			_ = r.withServerGate(context.Background(), server, func() error { return nil })
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("%s: a parallel server must not gate a second concurrent call", server)
		}
		close(released)
	}
}

// A queued call abandons the gate when its own run is cancelled rather than
// pinning the whole session behind a stuck server.
func TestMCPSerialGateHonoursCancellation(t *testing.T) {
	r := runtimeWithServers(config.PluginEntry{Name: "browser"})
	held := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_ = r.withServerGate(context.Background(), "browser", func() error {
			close(held)
			<-released
			return nil
		})
	}()
	<-held
	defer close(released)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	err := r.withServerGate(ctx, "browser", func() error { ran = true; return nil })
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation surfaced", err)
	}
	if ran {
		t.Fatal("a cancelled call must not execute after failing to take the gate")
	}
}
