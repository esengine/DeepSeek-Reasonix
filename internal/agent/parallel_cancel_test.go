package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// stubbornTool ignores its context: it returns only when released.
type stubbornTool struct {
	once    *sync.Once
	started chan struct{}
	release chan struct{}
}

func (stubbornTool) Name() string            { return "stubborn" }
func (stubbornTool) Description() string     { return "ignores cancellation" }
func (stubbornTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (stubbornTool) ReadOnly() bool          { return true }
func (s stubbornTool) Execute(context.Context, json.RawMessage) (string, error) {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return "late", nil
}

// fastTool reports when it has entered execution, so the test cancels only
// after both tools of the batch are running.
type fastTool struct {
	once    *sync.Once
	started chan struct{}
}

func (fastTool) Name() string            { return "fast" }
func (fastTool) Description() string     { return "always succeeds" }
func (fastTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fastTool) ReadOnly() bool          { return true }
func (f fastTool) Execute(context.Context, json.RawMessage) (string, error) {
	f.once.Do(func() { close(f.started) })
	return "ok", nil
}

// A read-only parallel segment must not keep the whole turn wedged behind one
// tool that ignores cancellation: after the grace the batch reports that call
// as an unknown effect while the calls that did finish keep their results.
func TestParallelBatchAbandonsToolThatIgnoresCancellation(t *testing.T) {
	oldGrace := parallelStragglerGrace
	parallelStragglerGrace = 200 * time.Millisecond
	t.Cleanup(func() { parallelStragglerGrace = oldGrace })

	stub := stubbornTool{once: &sync.Once{}, started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(stub.release) })
	fast := fastTool{once: &sync.Once{}, started: make(chan struct{})}
	reg := tool.NewRegistry()
	reg.Add(stub)
	reg.Add(fast)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("stubborn-1", "stubborn", `{}`), toolCallChunk("fast-1", "fast", `{}`)},
		{{Type: provider.ChunkText, Text: "done"}},
	}}
	sess := NewSession("")
	a := New(prov, reg, sess, Options{}, &recordSink{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(withNoClosedLoop(ctx), "go") }()
	select {
	case <-stub.started:
	case <-time.After(5 * time.Second):
		t.Fatal("stubborn tool never started")
	}
	select {
	case <-fast.started:
	case <-time.After(5 * time.Second):
		t.Fatal("fast tool never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled batch stayed wedged behind a tool that ignores its context")
	}
	if got := toolResultByID(sess, "stubborn-1"); !strings.Contains(got, "did not stop after cancellation") {
		t.Fatalf("stubborn result = %q, want the abandoned marker", got)
	}
	if got := toolResultByID(sess, "fast-1"); !strings.Contains(got, "ok") {
		t.Fatalf("fast result = %q, want the finished tool's own output", got)
	}
}
