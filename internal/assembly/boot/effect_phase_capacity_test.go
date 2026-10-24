package boot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
)

type phaseEffectProvider struct {
	mu      sync.Mutex
	calls   map[string]int
	waiting <-chan struct{}
	reached chan struct{}
	once    sync.Once
	nested  bool
}

func (*phaseEffectProvider) Name() string { return "phase-capacity-effect" }

func (p *phaseEffectProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	role := "root"
	for _, marker := range []string{"PHASE_NESTED", "PHASE_READER", "PHASE_WRITER", "PHASE_PARENT"} {
		if strings.Contains(firstUser(req), marker) {
			role = marker
			break
		}
	}
	p.mu.Lock()
	call := p.calls[role]
	p.calls[role]++
	p.mu.Unlock()
	var toolCall *provider.ToolCall
	switch {
	case role == "root" && call == 0 && p.nested:
		toolCall = &provider.ToolCall{ID: "parent", Name: "task", Arguments: `{"prompt":"PHASE_PARENT"}`}
	case role == "root" && call == 0:
		toolCall = &provider.ToolCall{ID: "writer", Name: "task", Arguments: `{"prompt":"PHASE_WRITER","run_in_background":true}`}
	case role == "root" && call == 1 && !p.nested:
		select {
		case <-p.waiting:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		toolCall = &provider.ToolCall{ID: "reader", Name: "task", Arguments: `{"prompt":"PHASE_READER","read_only":true}`}
	case role == "PHASE_WRITER" && call == 0:
		toolCall = &provider.ToolCall{ID: "write-blocked", Name: "write_file", Arguments: `{"path":"blocked.txt","content":"child output"}`}
	case role == "PHASE_PARENT" && call == 0:
		toolCall = &provider.ToolCall{ID: "nested", Name: "task", Arguments: `{"prompt":"PHASE_NESTED","read_only":true}`}
	case role == "PHASE_READER" || role == "PHASE_NESTED":
		p.once.Do(func() { close(p.reached) })
	}
	ch := make(chan provider.Chunk, 2)
	if toolCall != nil {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: toolCall}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "finished"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectPhaseCapacityAllowsWorkPastFileWaitAndNestedParent(t *testing.T) {
	var current *phaseEffectProvider
	provider.Register("phase-capacity-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	for _, nested := range []bool{false, true} {
		name := "blocked_writer"
		if nested {
			name = "nested_limit_one"
		}
		t.Run(name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			waiting := make(chan struct{})
			p := &phaseEffectProvider{calls: map[string]int{}, waiting: waiting, reached: make(chan struct{}), nested: nested}
			current = p
			writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
collaboration="shared"
max_subagent_concurrency=1
max_parallel_writers=1
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="phase-capacity-effect"
model="x"
`)
			mirrorToUserConfigAt(t, home, root)
			approveWorkspaceAt(t, home, root)
			var waited sync.Once
			c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.WorkspaceLease != nil && e.WorkspaceLease.State == "waiting" && e.WorkspaceLease.CallID == "write-blocked" {
					waited.Do(func() { close(waiting) })
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.EnsureSessionPath()
			c.SetToolApprovalMode(control.ToolApprovalYolo)
			other, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			held, err := other.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(root, "blocked.txt")}})
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(mutation.NotExecuted)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.RunTurn(ctx, "Run the capacity fixture") }()
			defer func() { cancel(); <-done }()
			select {
			case <-p.reached:
			case <-ctx.Done():
				t.Fatal("a waiting file mutation or parent task exhausted all model capacity")
			}
		})
	}
}
