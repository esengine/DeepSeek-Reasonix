package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestBackgroundFleetRegistersEveryWriterUntilCompletion(t *testing.T) {
	root := t.TempDir()
	prov := &fleetHoldProvider{started: make(chan struct{}, 2), release: make(chan struct{})}
	store := checkpoint.New("", root)
	observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(2, 2)).
		WithMutationObserver(observer)
	fleet := NewFleetTool(task)
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := withCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	ctx = jobs.WithManager(ctx, manager)
	ctx = jobs.WithSession(ctx, "parent-session")
	args := json.RawMessage(`{
		"run_in_background":true,
		"tasks":[
			{"prompt":"first","write_paths":["first.md"]},
			{"prompt":"second","write_paths":["second.md"]}
		]
	}`)
	if _, err := fleet.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-prov.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for background fleet writer")
		}
	}
	if writers := observer.ActiveWriters(); len(writers) != 3 {
		t.Fatalf("active fleet writers = %+v, want two item writers plus one fleet reservation", writers)
	}
	running := manager.RunningForSession("parent-session")
	if len(running) != 1 {
		t.Fatalf("running fleet jobs = %+v, want 1", running)
	}
	close(prov.release)
	result := manager.WaitForSession(context.Background(), "parent-session", []string{running[0].ID}, 5)
	if len(result) != 1 || result[0].Status != jobs.Done {
		t.Fatalf("background fleet result = %+v", result)
	}
	if writers := observer.ActiveWriters(); len(writers) != 0 {
		t.Fatalf("fleet writers still registered after completion: %+v", writers)
	}
}

// TestBackgroundFleetProgressLifecycleUsesStableIDs guards both sides of the
// background handoff: Execute must leave the shared merger alive for the job,
// and group/child progress must be emitted through the raw parent sink so IDs
// are namespaced exactly once and match the cards already dispatched.
func TestBackgroundFleetProgressLifecycleUsesStableIDs(t *testing.T) {
	root := t.TempDir()
	rec := &recordSink{}
	prov := &fleetHoldProvider{started: make(chan struct{}, 2), release: make(chan struct{})}
	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(2, 2))
	fleet := NewFleetTool(task)
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := withCallContext(context.Background(), "fleet-call", rec, nil, false)
	ctx = jobs.WithManager(ctx, manager)
	ctx = jobs.WithSession(ctx, "progress-session")
	args := json.RawMessage(`{
		"run_in_background":true,
		"tasks":[
			{"prompt":"first","write_paths":["first.md"]},
			{"prompt":"second","write_paths":["second.md"]}
		]
	}`)
	if _, err := fleet.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-prov.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for background fleet child")
		}
	}
	running := manager.RunningForSession("progress-session")
	if len(running) != 1 {
		t.Fatalf("running fleet jobs = %+v, want 1", running)
	}
	close(prov.release)
	result := manager.WaitForSession(context.Background(), "progress-session", []string{running[0].ID}, 5)
	if len(result) != 1 || result[0].Status != jobs.Done {
		t.Fatalf("background fleet result = %+v, want one completed job", result)
	}

	groupStatuses := []string{}
	childStatuses := map[string][]string{}
	childPreviews := map[string]bool{}
	for _, e := range rec.kinds(event.ToolProgress) {
		if strings.Contains(e.Tool.ID, "fleet-call/fleet-call") {
			t.Fatalf("progress ID was namespaced twice: %+v", e.Tool)
		}
		switch {
		case e.Tool.ID == "fleet-call" && progressName(e) == event.SubagentProgressStatusName:
			if e.Tool.ParentID != "" {
				t.Fatalf("group progress ParentID = %q, want empty", e.Tool.ParentID)
			}
			groupStatuses = append(groupStatuses, progressOutput(e))
		case strings.HasPrefix(e.Tool.ID, "fleet-call/fleet-"):
			if e.Tool.ParentID != "fleet-call" {
				t.Fatalf("child progress ParentID = %q, want fleet-call", e.Tool.ParentID)
			}
			if progressName(e) == event.SubagentProgressStatusName {
				childStatuses[e.Tool.ID] = append(childStatuses[e.Tool.ID], progressOutput(e))
			}
			if progressName(e) == event.SubagentProgressTextName && progressOutput(e) != "" {
				childPreviews[e.Tool.ID] = true
			}
		}
	}
	if len(groupStatuses) != 2 || groupStatuses[0] != string(subagentPhaseRunning) || groupStatuses[1] != string(subagentPhaseCompleted) {
		t.Fatalf("group lifecycle = %v, want running → completed", groupStatuses)
	}
	for _, id := range []string{"fleet-call/fleet-1", "fleet-call/fleet-2"} {
		statuses := childStatuses[id]
		if len(statuses) < 2 || statuses[0] != string(subagentPhaseRunning) || statuses[len(statuses)-1] != string(subagentPhaseCompleted) {
			t.Fatalf("child %s lifecycle = %v, want running → … → completed", id, statuses)
		}
		terminals := 0
		for _, status := range statuses {
			if isTerminalStatusOutput(status) {
				terminals++
			}
		}
		if terminals != 1 {
			t.Fatalf("child %s terminals = %d, want exactly one", id, terminals)
		}
		if !childPreviews[id] {
			t.Fatalf("child %s never emitted its text preview", id)
		}
	}
}

func TestBackgroundFleetRegistersReservationWhileItemsAreQueued(t *testing.T) {
	root := t.TempDir()
	store := checkpoint.New("", root)
	observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
	scheduler := NewSubagentScheduler(1, 1)
	releaseSlot, err := scheduler.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(scheduler).
		WithMutationObserver(observer)
	fleet := NewFleetTool(task)
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := withCallContext(context.Background(), "queued-fleet", event.Discard, nil, false)
	ctx = jobs.WithManager(ctx, manager)
	ctx = jobs.WithSession(ctx, "queued-session")
	args := json.RawMessage(`{
		"run_in_background":true,
		"tasks":[
			{"prompt":"first","write_paths":["first.md"]},
			{"prompt":"second","write_paths":["second.md"]}
		]
	}`)
	if _, err := fleet.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	writers := observer.ActiveWriters()
	if len(writers) != 1 || writers[0].Kind != "background_fleet" {
		t.Fatalf("queued fleet reservation = %+v, want one rewind exclusion", writers)
	}
	releaseSlot()
	running := manager.RunningForSession("queued-session")
	if len(running) != 1 {
		t.Fatalf("running fleet jobs = %+v, want 1", running)
	}
	result := manager.WaitForSession(context.Background(), "queued-session", []string{running[0].ID}, 5)
	if len(result) != 1 || result[0].Status != jobs.Done {
		t.Fatalf("background fleet result = %+v, want one completed job", result)
	}
	if writers := observer.ActiveWriters(); len(writers) != 0 {
		t.Fatalf("completed background fleet still registered: %+v", writers)
	}
}

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
	for i := range 4 {
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

func TestFleetAggregatePreservesEveryReferenceUnderToolLimit(t *testing.T) {
	results := make([]fleetItemResult, 3)
	for i := range results {
		results[i] = fleetItemResult{
			index:  i,
			status: fleetItemCompleted,
			output: fmt.Sprintf("BEGIN-%d\n%s\nEND-%d", i+1, strings.Repeat(string(rune('a'+i)), 20*1024), i+1),
			ref:    fmt.Sprintf("sa_result_%d", i+1),
		}
	}
	out := formatFleetAggregate(results, false)
	if len(out) > subagentAggregateBudgetBytes {
		t.Fatalf("aggregate bytes = %d, want <= %d", len(out), subagentAggregateBudgetBytes)
	}
	if _, notice := truncateToolOutput(out); notice != "" {
		t.Fatalf("bounded fleet aggregate still hit generic truncation: %s", notice)
	}
	for i := range results {
		if !strings.Contains(out, results[i].ref) {
			t.Fatalf("aggregate lost ref %q", results[i].ref)
		}
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

type fleetHoldProvider struct {
	started chan struct{}
	release chan struct{}
}

func (p *fleetHoldProvider) Name() string { return "fleet-hold" }

func (p *fleetHoldProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.started <- struct{}{}
	<-p.release
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
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
