package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// failTool always errors with the same message regardless of its arguments,
// standing in for a call the model keeps re-emitting (e.g. arguments truncated at
// the output-token ceiling, which fail to parse the same way every time even as
// the model re-words the payload).
type failTool struct{ name string }

func (f failTool) Name() string            { return f.name }
func (f failTool) Description() string     { return "always fails" }
func (f failTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f failTool) ReadOnly() bool          { return true }
func (f failTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("unexpected end of JSON input")
}

// okTool always succeeds — a turn of real progress that breaks a failing run.
type okTool struct{ name string }

func (o okTool) Name() string                                             { return o.name }
func (o okTool) Description() string                                      { return "always succeeds" }
func (o okTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (o okTool) ReadOnly() bool                                           { return true }
func (o okTool) Execute(context.Context, json.RawMessage) (string, error) { return "ok", nil }

func noticeRecorder() (event.Sink, *[]string) {
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})
	return sink, &notices
}

// TestStormBreakerEscalatesRepeatedFailure: once the same tool has failed the
// same way stormBreakThreshold times in a row, the model-facing result must carry
// the loop-guard directive (not just the raw error again), and the user must get
// a notice. The arguments DIFFER on every call — mirroring the live failure
// mode where a stuck model re-words the payload — to prove detection keys on the
// error, not the bytes.
func TestStormBreakerEscalatesRepeatedFailure(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	args := []string{`{"content":"Mountains are`, `{"path":"n.txt","content":"Peaks rise`, `{}`}
	var last string
	for i := 0; i < stormBreakThreshold; i++ {
		call := provider.ToolCall{Name: "write_file", Arguments: args[i]}
		last = executeBatchOutputs(a, context.Background(), []provider.ToolCall{call})[0]
	}

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("after %d same-error failures the result should carry the loop guard, got: %q", stormBreakThreshold, last)
	}
	if !strings.Contains(last, "write_file") {
		t.Errorf("loop-guard text should name the offending tool, got: %q", last)
	}
	if !strings.Contains(last, "unexpected end of JSON input") {
		t.Errorf("loop-guard result should still preserve the original error, got: %q", last)
	}
	if len(*notices) == 0 {
		t.Errorf("loop guard should emit a notice to the user")
	}
}

// TestStormBreakerEscalatesRepeatedBlockedPermission covers the readiness
// recovery failure mode where the model keeps changing bash commands after the
// host returns the same permission denial. Blocked calls used to reset the storm
// counter, so this loop could churn approval prompts without ever changing
// approach.
func TestStormBreakerEscalatesRepeatedBlockedPermission(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{
		Gate: &stubGate{deny: map[string]bool{"bash": true}},
	}, sink)

	args := []string{
		`{"command":"go test ./..."}`,
		`{"command":"git status --short"}`,
		`{"command":"ls -la"}`,
	}
	var last string
	for i := 0; i < stormBreakThreshold; i++ {
		call := provider.ToolCall{Name: "bash", Arguments: args[i]}
		last = executeBatchOutputs(a, context.Background(), []provider.ToolCall{call})[0]
	}

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("after %d same permission blocks the result should carry the loop guard, got: %q", stormBreakThreshold, last)
	}
	if !strings.Contains(last, "blocked") || !strings.Contains(last, "permission") {
		t.Fatalf("permission loop guard should preserve blocked context, got: %q", last)
	}
	if len(*notices) == 0 {
		t.Errorf("loop guard should emit a notice to the user")
	}
}

// TestStormBreakerEscalatesAlternatingBlockedShapes: rotating between two
// blocked tools defeats the signature detector (each turn resets the count),
// but every turn is still a host refusal with zero progress. The blocked-turn
// streak must trip the guard anyway.
func TestStormBreakerEscalatesAlternatingBlockedShapes(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(fakeTool{name: "web_fetch", readOnly: false})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{
		Gate: &stubGate{deny: map[string]bool{"bash": true, "web_fetch": true}},
	}, sink)

	calls := []provider.ToolCall{
		{Name: "bash", Arguments: `{"command":"go test ./..."}`},
		{Name: "web_fetch", Arguments: `{"url":"https://example.com"}`},
		{Name: "bash", Arguments: `{"command":"ls"}`},
	}
	var last string
	for _, call := range calls {
		last = executeBatchOutputs(a, context.Background(), []provider.ToolCall{call})[0]
	}

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("after %d all-blocked turns the guard should fire despite alternating tools, got: %q", stormBreakThreshold, last)
	}
	if !a.loopGuardArmed {
		t.Fatal("streak guard should arm the final-readiness loop-guard pass")
	}
	if len(*notices) == 0 {
		t.Errorf("streak loop guard should emit a notice to the user")
	}
}

// TestStormBreakerBlockedStreakResetBySuccess: a turn that makes real progress
// proves the model is not stuck, so the blocked-turn streak must start over.
func TestStormBreakerBlockedStreakResetBySuccess(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	a := New(nil, reg, NewSession(""), Options{
		Gate: &stubGate{deny: map[string]bool{"bash": true}},
	}, event.Discard)

	calls := []provider.ToolCall{
		{Name: "bash", Arguments: `{"command":"go test ./..."}`},
		{Name: "bash", Arguments: `{"command":"ls"}`},
		{Name: "read_file", Arguments: `{"path":"a.go"}`},
		{Name: "bash", Arguments: `{"command":"pwd"}`},
	}
	var last string
	for _, call := range calls {
		last = executeBatchOutputs(a, context.Background(), []provider.ToolCall{call})[0]
	}

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("a successful turn should reset the blocked streak, got: %q", last)
	}
	if a.blockedTurnStreak != 1 {
		t.Fatalf("blockedTurnStreak = %d, want 1 after success reset plus one block", a.blockedTurnStreak)
	}
}

// TestStormBreakerEscalatesRepeatedBatch: a multi-call batch that fails the same
// way every round is just as much a death-spiral as a single call — once the whole
// batch repeats stormBreakThreshold times, the guard must fire and name the batch.
func TestStormBreakerEscalatesRepeatedBatch(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_a"})
	reg.Add(failTool{name: "write_b"})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	batch := []provider.ToolCall{
		{Name: "write_a", Arguments: `{"content":"x`},
		{Name: "write_b", Arguments: `{"content":"y`},
	}
	var first string
	for i := 0; i < stormBreakThreshold; i++ {
		first = executeBatchOutputs(a, context.Background(), batch)[0]
	}

	if !strings.Contains(first, "[loop guard]") {
		t.Fatalf("a repeated all-failing batch should trip the guard, got: %q", first)
	}
	if !strings.Contains(first, "batch of 2") {
		t.Errorf("guard should name the repeated batch, got: %q", first)
	}
	if len(*notices) == 0 {
		t.Errorf("loop guard should emit a notice for a repeated batch")
	}
}

// TestStormBreakerBatchResetsOnPartialSuccess: a batch where even one call
// succeeds is progress, not a fixation — the guard must never fire, however many
// times the batch repeats.
func TestStormBreakerBatchResetsOnPartialSuccess(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	reg.Add(okTool{name: "read_file"})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	batch := []provider.ToolCall{
		{Name: "write_file", Arguments: `{"content":"x`},
		{Name: "read_file", Arguments: `{"path":"x"}`},
	}
	var first string
	for i := 0; i < stormBreakThreshold+2; i++ {
		first = executeBatchOutputs(a, context.Background(), batch)[0]
	}

	if strings.Contains(first, "[loop guard]") {
		t.Fatalf("a batch with a succeeding call should never trip the guard, got: %q", first)
	}
	if len(*notices) != 0 {
		t.Errorf("no notice expected when part of the batch succeeds, got %v", *notices)
	}
}

// TestStormBreakerSilentBelowThreshold: the first few self-corrections are
// healthy — the guard must not fire before the threshold.
func TestStormBreakerSilentBelowThreshold(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	call := provider.ToolCall{Name: "write_file", Arguments: `{"content":"x`}
	var last string
	for i := 0; i < stormBreakThreshold-1; i++ {
		last = executeBatchOutputs(a, context.Background(), []provider.ToolCall{call})[0]
	}

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("guard fired after only %d repeats (threshold %d)", stormBreakThreshold-1, stormBreakThreshold)
	}
	if len(*notices) != 0 {
		t.Errorf("no notice expected below threshold, got %v", *notices)
	}
}

// TestStormBreakerResetsOnSuccess: a run of failures broken by a successful turn
// must reset the counter, so the guard does not fire prematurely afterward.
func TestStormBreakerResetsOnSuccess(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	reg.Add(okTool{name: "read_file"})
	sink, notices := noticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	fail := provider.ToolCall{Name: "write_file", Arguments: `{"content":"x`}
	good := provider.ToolCall{Name: "read_file", Arguments: `{"path":"x"}`}
	ctx := context.Background()

	a.executeBatch(ctx, []provider.ToolCall{fail})                    // count 1
	a.executeBatch(ctx, []provider.ToolCall{fail})                    // count 2
	a.executeBatch(ctx, []provider.ToolCall{good})                    // success → reset
	a.executeBatch(ctx, []provider.ToolCall{fail})                    // count 1
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{fail})[0] // count 2 — still below threshold

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("guard should have reset after a successful turn, got: %q", last)
	}
	if len(*notices) != 0 {
		t.Errorf("no notice expected when a success breaks the run, got %v", *notices)
	}
}

// executeBatchOutputs runs the batch and returns just the model-facing outputs.
func executeBatchOutputs(a *Agent, ctx context.Context, calls []provider.ToolCall) []string {
	outputs, _ := a.executeBatch(ctx, calls)
	return outputs
}
