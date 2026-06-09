package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const readinessE2EKind = "readiness-e2e-test"

var (
	readinessE2EOnce    sync.Once
	readinessE2EMu      sync.Mutex
	readinessE2ECurrent *readinessE2EProvider
)

func installReadinessE2EProvider(t *testing.T, p *readinessE2EProvider) {
	t.Helper()
	readinessE2EOnce.Do(func() {
		provider.Register(readinessE2EKind, func(provider.Config) (provider.Provider, error) {
			readinessE2EMu.Lock()
			defer readinessE2EMu.Unlock()
			if readinessE2ECurrent == nil {
				return nil, errors.New("readiness e2e provider not installed")
			}
			return readinessE2ECurrent, nil
		})
	})
	readinessE2EMu.Lock()
	readinessE2ECurrent = p
	readinessE2EMu.Unlock()
	t.Cleanup(func() {
		readinessE2EMu.Lock()
		if readinessE2ECurrent == p {
			readinessE2ECurrent = nil
		}
		readinessE2EMu.Unlock()
	})
}

// readinessE2EProvider scripts the #3664 shape: change a file and mark the step
// in_progress, then keep answering without ever signing it off.
type readinessE2EProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *readinessE2EProvider) Name() string { return "readiness-e2e" }

func (p *readinessE2EProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w1", Name: "write_file", Arguments: `{"path":"changed.go","content":"package main\n"}`}},
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "t1", Name: "todo_write", Arguments: `{"todos":[{"content":"Edit code","status":"in_progress"}]}`}},
			{Type: provider.ChunkDone},
		}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "all done (prematurely)"}, {Type: provider.ChunkDone}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (p *readinessE2EProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestReadinessGateDegradesEndToEnd exercises the whole real stack — boot.Build,
// the Controller, the agent loop, the builtin write_file/todo_write, the evidence
// ledger and the readiness gate — for #3664. A model that edits a file, marks the
// step in_progress, then keeps answering without signing off must not loop or
// abort: the turn degrades, the answer is delivered with a visible warning, and
// the real write actually lands in the workspace.
func TestReadinessGateDegradesEndToEnd(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &readinessE2EProvider{}
	installReadinessE2EProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "readiness-e2e-test"
model = "x"
`)

	var mu sync.Mutex
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx, "edit changed.go and report"); err != nil {
		t.Fatalf("turn must degrade gracefully end-to-end, got error: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("turn did not terminate — possible deadloop: %v", ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(dir, "changed.go")); err != nil {
		t.Fatalf("write_file did not run end-to-end: %v", err)
	}
	mu.Lock()
	joined := strings.Join(notices, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "proceeding without confirmed readiness") {
		t.Fatalf("want a visible readiness-degrade warning (not silent, not an error); notices = %v", notices)
	}
	if c := prov.callCount(); c > 4 {
		t.Fatalf("provider calls = %d, want bounded — the gate must cap the rounds", c)
	}
}
