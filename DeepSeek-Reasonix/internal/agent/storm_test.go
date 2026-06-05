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

func warnNoticeRecorder() (event.Sink, *[]string) {
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Level == event.LevelWarn {
			notices = append(notices, e.Text)
		}
	})
	return sink, &notices
}

// TestStormBreakerEscalatesRepeatedFailure: once the same tool has failed the
// same way stormBreakThreshold times in a row, the model-facing result must carry
// the loop-guard directive (not just the raw error again), and the user must get
// a warn notice. The arguments DIFFER on every call — mirroring the live failure
// mode where a stuck model re-words the payload — to prove detection keys on the
// error, not the bytes.
func TestStormBreakerEscalatesRepeatedFailure(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	sink, notices := warnNoticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	args := []string{`{"content":"Mountains are`, `{"path":"n.txt","content":"Peaks rise`, `{}`}
	var last string
	for i := 0; i < stormBreakThreshold; i++ {
		call := provider.ToolCall{Name: "write_file", Arguments: args[i]}
		last = a.executeBatch(context.Background(), []provider.ToolCall{call})[0]
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
		t.Errorf("loop guard should emit a warn notice to the user")
	}
}

// TestStormBreakerEscalatesRepeatedBatch: a multi-call batch that fails the same
// way every round is just as much a death-spiral as a single call — once the whole
// batch repeats stormBreakThreshold times, the guard must fire and name the batch.
func TestStormBreakerEscalatesRepeatedBatch(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_a"})
	reg.Add(failTool{name: "write_b"})
	sink, notices := warnNoticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	batch := []provider.ToolCall{
		{Name: "write_a", Arguments: `{"content":"x`},
		{Name: "write_b", Arguments: `{"content":"y`},
	}
	var first string
	for i := 0; i < stormBreakThreshold; i++ {
		first = a.executeBatch(context.Background(), batch)[0]
	}

	if !strings.Contains(first, "[loop guard]") {
		t.Fatalf("a repeated all-failing batch should trip the guard, got: %q", first)
	}
	if !strings.Contains(first, "batch of 2") {
		t.Errorf("guard should name the repeated batch, got: %q", first)
	}
	if len(*notices) == 0 {
		t.Errorf("loop guard should emit a warn notice for a repeated batch")
	}
}

// TestStormBreakerBatchResetsOnPartialSuccess: a batch where even one call
// succeeds is progress, not a fixation — the guard must never fire, however many
// times the batch repeats.
func TestStormBreakerBatchResetsOnPartialSuccess(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	reg.Add(okTool{name: "read_file"})
	sink, notices := warnNoticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	batch := []provider.ToolCall{
		{Name: "write_file", Arguments: `{"content":"x`},
		{Name: "read_file", Arguments: `{"path":"x"}`},
	}
	var first string
	for i := 0; i < stormBreakThreshold+2; i++ {
		first = a.executeBatch(context.Background(), batch)[0]
	}

	if strings.Contains(first, "[loop guard]") {
		t.Fatalf("a batch with a succeeding call should never trip the guard, got: %q", first)
	}
	if len(*notices) != 0 {
		t.Errorf("no warn notice expected when part of the batch succeeds, got %v", *notices)
	}
}

// TestStormBreakerSilentBelowThreshold: the first few self-corrections are
// healthy — the guard must not fire before the threshold.
func TestStormBreakerSilentBelowThreshold(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	sink, notices := warnNoticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	call := provider.ToolCall{Name: "write_file", Arguments: `{"content":"x`}
	var last string
	for i := 0; i < stormBreakThreshold-1; i++ {
		last = a.executeBatch(context.Background(), []provider.ToolCall{call})[0]
	}

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("guard fired after only %d repeats (threshold %d)", stormBreakThreshold-1, stormBreakThreshold)
	}
	if len(*notices) != 0 {
		t.Errorf("no warn notice expected below threshold, got %v", *notices)
	}
}

// TestStormBreakerResetsOnSuccess: a run of failures broken by a successful turn
// must reset the counter, so the guard does not fire prematurely afterward.
func TestStormBreakerResetsOnSuccess(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "write_file"})
	reg.Add(okTool{name: "read_file"})
	sink, notices := warnNoticeRecorder()
	a := New(nil, reg, NewSession(""), Options{}, sink)

	fail := provider.ToolCall{Name: "write_file", Arguments: `{"content":"x`}
	good := provider.ToolCall{Name: "read_file", Arguments: `{"path":"x"}`}
	ctx := context.Background()

	a.executeBatch(ctx, []provider.ToolCall{fail})            // count 1
	a.executeBatch(ctx, []provider.ToolCall{fail})            // count 2
	a.executeBatch(ctx, []provider.ToolCall{good})            // success → reset
	a.executeBatch(ctx, []provider.ToolCall{fail})            // count 1
	last := a.executeBatch(ctx, []provider.ToolCall{fail})[0] // count 2 — still below threshold

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("guard should have reset after a successful turn, got: %q", last)
	}
	if len(*notices) != 0 {
		t.Errorf("no warn notice expected when a success breaks the run, got %v", *notices)
	}
}
