package boot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type coalesceWiringProvider struct{ deltas int }

func (p *coalesceWiringProvider) Name() string { return "boot-coalesce-test" }

func (p *coalesceWiringProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, p.deltas+1)
	for i := range p.deltas {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf("w%d ", i)}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type coalesceRecordSink struct {
	mu     sync.Mutex
	texts  []string
	events int
}

func (s *coalesceRecordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == event.Text {
		s.events++
		s.texts = append(s.texts, e.Text)
	}
}

// TestBuildCoalescesAgentStreamDeltas pins the wiring, not the coalescer: the
// executor emits into the shared boot sink directly, so coalescing must wrap
// that sink — wrapping only the controller's reference leaves the per-chunk
// stream untouched (the regression this test exists for).
func TestBuildCoalescesAgentStreamDeltas(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	const deltas = 40
	provider.Register("boot-coalesce-test", func(provider.Config) (provider.Provider, error) {
		return &coalesceWiringProvider{deltas: deltas}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-coalesce-test"
model = "x"
`)

	sink := &coalesceRecordSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "stream"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sink.mu.Lock()
	events := sink.events
	joined := strings.Join(sink.texts, "")
	sink.mu.Unlock()

	var want strings.Builder
	for i := range deltas {
		fmt.Fprintf(&want, "w%d ", i)
	}
	if joined != want.String() {
		t.Fatalf("concatenated deltas = %q, want %q", joined, want.String())
	}
	if events >= deltas/2 {
		t.Fatalf("frontend sink saw %d text events for %d provider chunks — agent stream is not coalesced", events, deltas)
	}
}
