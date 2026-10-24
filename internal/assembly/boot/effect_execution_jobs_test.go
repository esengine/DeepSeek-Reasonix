package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type executionJobsProvider struct {
	mu        sync.Mutex
	parent    []provider.Request
	child     []provider.Request
	started   chan struct{}
	final     chan struct{}
	release   chan struct{}
	startOnce sync.Once
	finalOnce sync.Once
}

func (*executionJobsProvider) Name() string { return "owned-execution-jobs" }
func (p *executionJobsProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(name, args string) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate", Name: name, Arguments: args}}
	}
	switch {
	case strings.Contains(firstUser(req), "OWNED_BACKGROUND_CHILD"):
		p.mu.Lock()
		p.child = append(p.child, req)
		p.mu.Unlock()
		p.startOnce.Do(func() { close(p.started) })
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "child done"}
	case strings.Contains(firstUser(req), "OWNED_BACKGROUND_PARENT"):
		p.mu.Lock()
		p.parent = append(p.parent, req)
		p.mu.Unlock()
		if !hasToolResult(req) {
			call("task", `{"prompt":"OWNED_BACKGROUND_CHILD","run_in_background":true,"tools":["read_file"]}`)
		} else {
			p.finalOnce.Do(func() { close(p.final) })
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent done"}
		}
	default:
		if !hasToolResult(req) {
			call("task", `{"prompt":"OWNED_BACKGROUND_PARENT","isolation":"worktree","tools":["task","wait","bash","bash_output","kill_shell"]}`)
		} else {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "root done"}
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectIsolatedExecutionJoinsItsBackgroundDescendants(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	p := &executionJobsProvider{started: make(chan struct{}), final: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(p.release) })
	provider.Register("boot-execution-jobs", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
max_subagent_depth = 3
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-execution-jobs"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-q", "-m", "fixture")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	ctrl, err := Build(ctx, Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	returned := make(chan error, 1)
	go func() { returned <- ctrl.Run(ctx, "delegate an isolated parent with one background child") }()
	for _, signal := range []<-chan struct{}{p.started, p.final} {
		select {
		case <-signal:
		case err := <-returned:
			t.Fatalf("production delegation returned before descendant execution: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case err := <-returned:
		t.Fatalf("parent released a running descendant: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(p.release) })
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.parent) < 2 || len(p.child) == 0 {
		t.Fatal("real Build did not execute both delegation levels")
	}
	first, _ := json.Marshal(p.parent[0].Tools)
	last, _ := json.Marshal(p.parent[len(p.parent)-1].Tools)
	if !bytes.Equal(first, last) || p.parent[0].Messages[0].Content != p.parent[len(p.parent)-1].Messages[0].Content {
		t.Fatal("background activity changed the provider prefix")
	}
	for _, name := range []string{"wait", "bash_output", "kill_shell"} {
		found := false
		for _, schema := range p.parent[0].Tools {
			found = found || schema.Name == name
		}
		if !found {
			t.Errorf("owned execution omitted %s", name)
		}
	}
}
