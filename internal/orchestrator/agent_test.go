package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type mockRunner struct {
	runFn func(ctx context.Context, input string) error
}

func (m *mockRunner) Run(ctx context.Context, input string) error {
	if m.runFn != nil {
		return m.runFn(ctx, input)
	}
	return nil
}

func testController(t *testing.T, runner *mockRunner, msgs []provider.Message) *control.Controller {
	t.Helper()
	sess := agent.NewSession("")
	for _, m := range msgs {
		sess.Add(m)
	}
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	return control.New(control.Options{
		Sink:     event.Discard,
		Executor: exec,
		Runner:   runner,
	})
}

func TestNewManagedAgent(t *testing.T) {
	ctrl := testController(t, &mockRunner{}, nil)
	sink := NewSinkMultiplexer(event.Discard, "test-agent")
	a := NewManagedAgent("test-agent", ctrl, sink, config.OrchestratorAgentEntry{})

	if a.Name != "test-agent" {
		t.Fatalf("expected name 'test-agent', got %q", a.Name)
	}
	if a.Status() != StatusIdle {
		t.Fatalf("expected initial status %q, got %q", StatusIdle, a.Status())
	}
	if a.LastTask() != "" {
		t.Fatalf("expected empty last task, got %q", a.LastTask())
	}
	if a.LastResult() != "" {
		t.Fatalf("expected empty last result, got %q", a.LastResult())
	}
	if a.LastError() != "" {
		t.Fatalf("expected empty last error, got %q", a.LastError())
	}
}

func TestManagedAgentStatus(t *testing.T) {
	ctrl := testController(t, &mockRunner{}, nil)
	a := NewManagedAgent("test", ctrl, NewSinkMultiplexer(event.Discard, "test"), config.OrchestratorAgentEntry{})

	if a.Status() != StatusIdle {
		t.Fatalf("expected StatusIdle, got %q", a.Status())
	}

	a.setStatus(StatusRunning)
	if a.Status() != StatusRunning {
		t.Fatalf("expected StatusRunning, got %q", a.Status())
	}

	a.setStatus(StatusDone)
	if a.Status() != StatusDone {
		t.Fatalf("expected StatusDone, got %q", a.Status())
	}

	a.setStatus(StatusIdle)
	if a.Status() != StatusIdle {
		t.Fatalf("expected StatusIdle, got %q", a.Status())
	}
}

func TestManagedAgentLastTask(t *testing.T) {
	ctrl := testController(t, &mockRunner{}, nil)
	a := NewManagedAgent("test", ctrl, NewSinkMultiplexer(event.Discard, "test"), config.OrchestratorAgentEntry{})

	if a.LastTask() != "" {
		t.Fatalf("expected empty last task")
	}

	a.setLastTask("do something")
	if a.LastTask() != "do something" {
		t.Fatalf("expected 'do something', got %q", a.LastTask())
	}
}

func TestManagedAgentLastResult(t *testing.T) {
	ctrl := testController(t, &mockRunner{}, nil)
	a := NewManagedAgent("test", ctrl, NewSinkMultiplexer(event.Discard, "test"), config.OrchestratorAgentEntry{})

	a.setLastResult("result-value")
	if a.LastResult() != "result-value" {
		t.Fatalf("expected 'result-value', got %q", a.LastResult())
	}
}

func TestManagedAgentLastError(t *testing.T) {
	ctrl := testController(t, &mockRunner{}, nil)
	a := NewManagedAgent("test", ctrl, NewSinkMultiplexer(event.Discard, "test"), config.OrchestratorAgentEntry{})

	a.setLastError("something went wrong")
	if a.LastError() != "something went wrong" {
		t.Fatalf("expected 'something went wrong', got %q", a.LastError())
	}
}

func TestManagedAgentRunSuccess(t *testing.T) {
	runner := &mockRunner{}
	ctrl := testController(t, runner, []provider.Message{
		{Role: provider.RoleAssistant, Content: "the answer is 42"},
	})

	var mu sync.Mutex
	var emitted []event.Event
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		emitted = append(emitted, e)
		mu.Unlock()
	})

	mux := NewSinkMultiplexer(sink, "helper")
	a := NewManagedAgent("helper", ctrl, mux, config.OrchestratorAgentEntry{})

	result, err := a.Run(context.Background(), "calculate the answer")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result != "the answer is 42" {
		t.Fatalf("expected result 'the answer is 42', got %q", result)
	}
	if a.Status() != StatusIdle {
		t.Fatalf("expected StatusIdle after Run, got %q", a.Status())
	}
	if a.LastTask() != "calculate the answer" {
		t.Fatalf("expected last task 'calculate the answer', got %q", a.LastTask())
	}
	if a.LastResult() != "the answer is 42" {
		t.Fatalf("expected last result 'the answer is 42', got %q", a.LastResult())
	}
	if a.LastError() != "" {
		t.Fatalf("expected empty last error, got %q", a.LastError())
	}

	mu.Lock()
	if len(emitted) != 2 {
		t.Fatalf("expected 2 sink events, got %d", len(emitted))
	}
	if emitted[0].Kind != event.Notice {
		t.Fatalf("first event expected Notice, got %v", emitted[0].Kind)
	}
	if emitted[1].Kind != event.Notice {
		t.Fatalf("second event expected Notice, got %v", emitted[1].Kind)
	}
	mu.Unlock()
}

func TestManagedAgentRunError(t *testing.T) {
	someErr := errors.New("provider failure")
	runner := &mockRunner{
		runFn: func(ctx context.Context, input string) error {
			return someErr
		},
	}
	ctrl := testController(t, runner, []provider.Message{
		{Role: provider.RoleAssistant, Content: "partial result"},
	})

	mux := NewSinkMultiplexer(event.Discard, "helper")
	a := NewManagedAgent("helper", ctrl, mux, config.OrchestratorAgentEntry{})

	result, err := a.Run(context.Background(), "do the thing")
	if err == nil {
		t.Fatal("expected error from Run, got nil")
	}
	if result != "" {
		t.Fatalf("expected empty result on error, got %q", result)
	}
	if a.LastError() != "provider failure" {
		t.Fatalf("expected last error 'provider failure', got %q", a.LastError())
	}
}

func TestManagedAgentRunWithPersistence(t *testing.T) {
	runner := &mockRunner{}
	dir := t.TempDir()
	ctrl := testController(t, runner, []provider.Message{
		{Role: provider.RoleAssistant, Content: "persisted result"},
	})
	ctrl.SetSessionPath(filepath.Join(dir, "session.jsonl"))

	mux := NewSinkMultiplexer(event.Discard, "helper")
	a := NewManagedAgent("helper", ctrl, mux, config.OrchestratorAgentEntry{
		Persist: true,
	})

	result, err := a.Run(context.Background(), "my task")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result != "persisted result" {
		t.Fatalf("expected 'persisted result', got %q", result)
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected session files to be created by Snapshot")
	}
}

func TestManagedAgentRunSinkRecorder(t *testing.T) {
	runner := &mockRunner{}
	ctrl := testController(t, runner, nil)

	var emitted []event.Event
	mux := NewSinkMultiplexer(event.FuncSink(func(e event.Event) {
		emitted = append(emitted, e)
	}), "worker")
	a := NewManagedAgent("worker", ctrl, mux, config.OrchestratorAgentEntry{})

	_, err := a.Run(context.Background(), "do work")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(emitted) != 2 {
		t.Fatalf("expected 2 events (start notice + done notice), got %d", len(emitted))
	}
	if emitted[0].Kind != event.Notice {
		t.Fatalf("expected first event to be Notice, got %v", emitted[0].Kind)
	}
	if emitted[1].Kind != event.Notice {
		t.Fatalf("expected second event to be Notice, got %v", emitted[1].Kind)
	}
}
