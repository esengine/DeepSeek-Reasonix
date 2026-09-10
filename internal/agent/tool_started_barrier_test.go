package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A checked sink models the durable acknowledgement, independently of the
// display-only Emit path. Only acknowledged records count as start evidence.
type startBarrierSink struct {
	mu        sync.Mutex
	events    []event.Event
	failStart bool
}

func (s *startBarrierSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *startBarrierSink) EmitChecked(e event.Event) error {
	if e.Kind == event.ToolStarted && s.failStart {
		return errors.New("injected tool-start journal failure")
	}
	s.Emit(e)
	return nil
}

func (s *startBarrierSink) starts(callID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, e := range s.events {
		if e.Kind == event.ToolStarted && e.Tool.ID == callID {
			count++
		}
	}
	return count
}

func TestToolStartedNotEmittedForRejectedCalls(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		opts Options
	}{
		{name: "permission denied", args: `{"value":"ok"}`, opts: Options{Gate: &stubGate{deny: map[string]bool{"barrier_probe": true}}}},
		{name: "malformed arguments", args: `{"value":`},
		{name: "schema violation", args: `{}`},
		{name: "pre-execution hook denied", args: `{"value":"ok"}`, opts: Options{Hooks: &stubHooks{blockPre: map[string]bool{"barrier_probe": true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := &recoveryArgumentTool{name: "barrier_probe", schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}
			reg := tool.NewRegistry()
			reg.Add(target)
			sink := &startBarrierSink{}
			a := New(nil, reg, NewSession(""), tc.opts, sink)
			batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "rejected", Name: target.Name(), Arguments: tc.args}})
			if len(target.inputs) != 0 {
				t.Fatalf("rejected tool executed %d times", len(target.inputs))
			}
			if len(batch.outcomes) != 1 || batch.outcomes[0].errMsg == "" {
				t.Fatalf("expected rejection outcome, got %+v", batch)
			}
			if got := sink.starts("rejected"); got != 0 {
				t.Errorf("rejected call has %d durable start records; must remain unstarted", got)
			}
		})
	}
}

func TestToolStartedPersistenceFailureDoesNotExecuteOrBecomeUnknown(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "barrier_probe", readOnly: true, calls: &executions})
	sink := &startBarrierSink{failStart: true}
	a := New(nil, reg, NewSession(""), Options{}, sink)
	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "not-started", Name: "barrier_probe", Arguments: `{}`}})
	if got := atomic.LoadInt32(&executions); got != 0 {
		t.Fatalf("tool executed %d times despite failed start persistence", got)
	}
	if len(batch.outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(batch.outcomes))
	}
	if state := outcomeRunState(batch.outcomes[0]); state != provider.ToolRunNotStarted {
		t.Errorf("failed start persistence state = %q, want %q; execution never began", state, provider.ToolRunNotStarted)
	}
	if got := sink.starts("not-started"); got != 0 {
		t.Errorf("failed journal write created %d acknowledged records", got)
	}
}

type startObservingTool struct {
	sink *startBarrierSink
	seen atomic.Int32
}

func (*startObservingTool) Name() string            { return "barrier_probe" }
func (*startObservingTool) Description() string     { return "observe durable start at dispatch" }
func (*startObservingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*startObservingTool) ReadOnly() bool          { return true }
func (t *startObservingTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.seen.Store(int32(t.sink.starts("executed")))
	return "done", nil
}

func TestToolStartedAcknowledgedBeforeConcreteExecution(t *testing.T) {
	sink := &startBarrierSink{}
	target := &startObservingTool{sink: sink}
	reg := tool.NewRegistry()
	reg.Add(target)
	a := New(nil, reg, NewSession(""), Options{}, sink)
	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "executed", Name: target.Name(), Arguments: `{}`}})
	if batch.err != nil || len(batch.outcomes) != 1 || batch.outcomes[0].errMsg != "" {
		t.Fatalf("execution failed: %+v", batch)
	}
	if got := target.seen.Load(); got != 1 {
		t.Errorf("tool observed %d acknowledged starts at execution, want exactly 1", got)
	}
	if got := sink.starts("executed"); got != 1 {
		t.Errorf("call has %d starts after completion, want exactly 1", got)
	}
}
