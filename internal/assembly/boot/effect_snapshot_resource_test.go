package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectWideWorkspaceSnapshotResourceBound(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	for i := range 1024 {
		path := filepath.Join(dir, "node_modules", fmt.Sprintf("d%04d", i), "child")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resolver := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "fixture/chat", Model: "chat", Default: true}},
		Providers:   map[string]provider.Provider{"fixture/chat": &scriptedProvider{}},
	}
	approveWorkspace(t, dir)
	sink := &bundleAuditSink{}
	ctrl, err := Build(t.Context(), Options{WorkspaceRoot: dir, Sink: sink, ProviderResolver: resolver, HeadlessApprovalMode: control.ToolApprovalAuto})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	done := make(chan error, 1)
	go func() { done <- ctrl.Run(context.Background(), "reply") }()
	peak := 0
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			audits := sink.audits()
			if len(audits) != 1 || !audits[0].Sealed || !audits[0].SnapshotComplete {
				t.Fatalf("audits = %+v", audits)
			}
			if peak > walkPoolSize*concurrentWalkKinds {
				t.Fatalf("walk goroutines peaked at %d over %d directories, want <= %d (%d pool x %d walk kinds)",
					peak, 1024, walkPoolSize*concurrentWalkKinds, walkPoolSize, concurrentWalkKinds)
			}
			waitNoWalkGoroutines(t)
			return
		case <-tick.C:
			peak = max(peak, walkGoroutines())
		}
	}
}

// walkPoolSize is the per-walk worker bound in observation and agent; the
// walk's calling goroutine is one of the pool.
const walkPoolSize = 16

// concurrentWalkKinds counts the walkers a turn may run at once: the
// observation snapshot and the agent's mutation scan.
const concurrentWalkKinds = 2

// walkGoroutines counts live goroutines running a workspace walk, so host
// background work (history catalog, database watchers) never counts against
// the bound.
func walkGoroutines() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "reasonix/internal/state/observation.") || strings.Contains(g, "reasonix/internal/runtime/agent.scanWorkspaceTo") {
			count++
		}
	}
	return count
}

func waitNoWalkGoroutines(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for walkGoroutines() > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d walk goroutines still alive after the turn settled", walkGoroutines())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
