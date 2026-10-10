package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type pathLeaseProvider struct {
	path    string
	written chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (*pathLeaseProvider) Name() string { return "path-lease-effect" }

func (p *pathLeaseProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		p.once.Do(func() { close(p.written) })
		select {
		case <-p.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	} else {
		args, _ := json.Marshal(map[string]string{"path": p.path, "content": "fixture\n"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectTwoControllersWriteDisjointPathsInOneWorkspace(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	root := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "path-lease-effect"
model = "x"
`)
	approveWorkspaceAt(t, home, root)
	first := &pathLeaseProvider{path: "first.txt", written: make(chan struct{}), resume: make(chan struct{})}
	second := &pathLeaseProvider{path: "second.txt", written: make(chan struct{}), resume: make(chan struct{})}
	current := first
	provider.Register("path-lease-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	build := func() *control.Controller {
		c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(func() { c.Close() })
		return c
	}
	a := build()
	current = second
	b := build()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doneA, doneB := make(chan struct{}), make(chan struct{})
	defer func() { cancel(); close(first.resume); close(second.resume); <-doneA; <-doneB }()
	go func() { defer close(doneA); _ = a.Run(ctx, "create first.txt") }()
	go func() {
		defer close(doneB)
		select {
		case <-first.written:
		case <-ctx.Done():
			return
		}
		_ = b.Run(ctx, "create second.txt")
	}()
	select {
	case <-second.written:
	case <-ctx.Done():
		t.Fatal("second controller could not write while first turn remained active")
	}
	if !a.WorkspaceLeaseState().Acquired || !b.WorkspaceLeaseState().Acquired {
		t.Fatal("both controllers must retain their independent write leases")
	}
	for _, name := range []string{"first.txt", "second.txt"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "fixture\n" {
			t.Fatalf("%s: %q, %v", name, data, err)
		}
	}
}
