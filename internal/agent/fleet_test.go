package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestFleetSchemaStableAndBounds(t *testing.T) {
	f := NewFleetTool(&TaskTool{})
	schema := string(f.Schema())
	for _, want := range []string{`"profile"`, `"write_paths"`, `"read_only"`, `"run_in_background"`} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %s: %s", want, schema)
		}
	}
	// Profile names must not be enumerated in schema (cache stability).
	if strings.Contains(schema, "doc-rewriter") || strings.Contains(schema, "enum") {
		t.Fatalf("schema must not embed profile names: %s", schema)
	}
	if f.Name() != "fleet" {
		t.Fatalf("name = %q", f.Name())
	}
}

func TestFleetRejectsSingleTaskAndPathConflict(t *testing.T) {
	root := t.TempDir()
	task := newTestTaskTool(t, &mockProvider{name: "sub"}, tool.NewRegistry(), "sys", "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(6, 3))
	f := NewFleetTool(task)

	_, err := f.Execute(context.Background(), json.RawMessage(`{"tasks":[{"prompt":"only one"}]}`))
	if err == nil || !strings.Contains(err.Error(), "between") {
		t.Fatalf("single task error = %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"tasks": []map[string]any{
			{"prompt": "a", "write_paths": []string{"same.md"}},
			{"prompt": "b", "write_paths": []string{"same.md"}},
		},
	})
	_, err = f.Execute(withCallContext(context.Background(), "fleet-call", event.Discard, nil, false), args)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("path conflict error = %v", err)
	}

	// Read-only items must not shift the caller-visible task numbers in the
	// preflight diagnostic.
	args, _ = json.Marshal(map[string]any{
		"tasks": []map[string]any{
			{"prompt": "inspect", "read_only": true},
			{"prompt": "writer a", "write_paths": []string{"same.md"}},
			{"prompt": "writer b", "write_paths": []string{"same.md"}},
		},
	})
	_, err = f.Execute(withCallContext(context.Background(), "fleet-call", event.Discard, nil, false), args)
	if err == nil || !strings.Contains(err.Error(), "task 2 and task 3") {
		t.Fatalf("mixed-task conflict error = %v, want original task numbers 2 and 3", err)
	}
}

func TestFleetCancellationPreservesStartedItemStatus(t *testing.T) {
	root := t.TempDir()
	prov := &fleetCancelProvider{
		started:  make(chan struct{}, 2),
		observed: make(chan struct{}, 2),
		release:  make(chan struct{}),
	}
	reg := tool.NewRegistry()
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(2, 2))
	f := NewFleetTool(task)

	ctx, cancel := context.WithCancel(withCallContext(context.Background(), "fleet-call", event.Discard, nil, false))
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := f.Execute(ctx, json.RawMessage(`{
			"tasks":[
				{"prompt":"first","write_paths":["first.md"]},
				{"prompt":"second","write_paths":["second.md"]}
			]
		}`))
		done <- struct {
			out string
			err error
		}{out: out, err: err}
	}()

	// Both workers are inside the provider before cancellation. Hold their
	// terminal results until the fleet has observed ctx.Done, then release them.
	waitSignal := func(name string, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", name)
		}
	}
	for range 2 {
		waitSignal("provider start", prov.started)
	}
	cancel()
	for range 2 {
		waitSignal("provider cancellation", prov.observed)
	}
	close(prov.release)

	var got struct {
		out string
		err error
	}
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fleet cancellation result")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("fleet error = %v, want context.Canceled", got.err)
	}
	if strings.Contains(got.out, "status: skipped") {
		t.Fatalf("started tasks must not be reported skipped after cancellation:\n%s", got.out)
	}
	if count := strings.Count(got.out, "status: cancelled"); count != 2 {
		t.Fatalf("cancelled status count = %d, want 2:\n%s", count, got.out)
	}
}

func TestFleetParallelDisjointWriters(t *testing.T) {
	root := t.TempDir()
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	prov := &fleetBarrierProvider{
		onPrompt: func() {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			concurrent.Add(-1)
		},
	}
	reg := tool.NewRegistry()
	// No writer tools needed — provider finishes without tools.
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(10, 10))
	f := NewFleetTool(task)

	tasks := make([]map[string]any, 0, 4)
	for i := 0; i < 4; i++ {
		path := filepath.Join("docs", "f"+string(rune('0'+i))+".md")
		tasks = append(tasks, map[string]any{
			"prompt":      "handle " + path,
			"write_paths": []string{path},
			"description": path,
		})
	}
	args, _ := json.Marshal(map[string]any{"tasks": tasks})
	ctx := withCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	out, err := f.Execute(ctx, args)
	if err != nil {
		t.Fatalf("fleet: %v", err)
	}
	if !strings.Contains(out, "Completed fleet of 4") {
		t.Fatalf("output = %s", out)
	}
	if maxConcurrent.Load() < 2 {
		t.Fatalf("expected concurrent starts, max=%d", maxConcurrent.Load())
	}
}

type fleetBarrierProvider struct {
	onPrompt func()
}

type fleetCancelProvider struct {
	started  chan struct{}
	observed chan struct{}
	release  chan struct{}
}

func (p *fleetCancelProvider) Name() string { return "fleet-cancel" }

func (p *fleetCancelProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.started <- struct{}{}
	<-ctx.Done()
	p.observed <- struct{}{}
	<-p.release
	return nil, ctx.Err()
}

func (p *fleetBarrierProvider) Name() string { return "fleet-barrier" }

func (p *fleetBarrierProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.onPrompt != nil {
		p.onPrompt()
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
}

func mustSubagentStore(t *testing.T) *SubagentStore {
	t.Helper()
	return NewSubagentStore(t.TempDir())
}
