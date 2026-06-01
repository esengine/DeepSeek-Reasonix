package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

// TestTruncateToolOutputUnderCap leaves small payloads alone — the cap should
// never rewrite content that already fits.
func TestTruncateToolOutputUnderCap(t *testing.T) {
	in := strings.Repeat("a", maxToolOutputBytes)
	got, notice := truncateToolOutput(in)
	if got != in {
		t.Errorf("payload at exactly the cap was rewritten")
	}
	if notice != "" {
		t.Errorf("at-cap payload should not emit a notice, got %q", notice)
	}
}

// TestTruncateToolOutputHeadTail keeps head+tail of an oversize payload and
// inserts a marker; the notice must report the elided byte count truthfully.
func TestTruncateToolOutputHeadTail(t *testing.T) {
	head := strings.Repeat("H", maxToolOutputBytes)
	tail := strings.Repeat("T", maxToolOutputBytes)
	in := head + tail
	out, notice := truncateToolOutput(in)
	if !strings.HasPrefix(out, "H") || !strings.HasSuffix(out, "T") {
		t.Errorf("head/tail not preserved at the edges: %q…%q", out[:20], out[len(out)-20:])
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("truncation marker missing: %q", out)
	}
	if len(out) >= len(in) {
		t.Errorf("output not shorter than input: in=%d out=%d", len(in), len(out))
	}
	if !strings.Contains(notice, "truncated") {
		t.Errorf("notice missing: %q", notice)
	}
}

// TestTruncateToolOutputRuneBoundaries puts multibyte runes exactly across the
// head and tail cut points; the result must still be valid UTF-8.
func TestTruncateToolOutputRuneBoundaries(t *testing.T) {
	in := strings.Repeat("中", maxToolOutputBytes) // 3 bytes each — guarantees a cut inside a rune
	out, _ := truncateToolOutput(in)
	if !utf8.ValidString(out) {
		t.Errorf("truncated output is not valid UTF-8")
	}
}

// TestFinishReasonMessage only yields a warning for abnormal terminations.
// Normal stops are silent (ok=false) so the per-turn line stays clean.
func TestFinishReasonMessage(t *testing.T) {
	silent := []string{"", "stop", "tool_calls"}
	for _, r := range silent {
		if msg, ok := finishReasonMessage(&provider.Usage{FinishReason: r}); ok {
			t.Errorf("finish_reason=%q should be silent, got %q", r, msg)
		}
	}
	loud := map[string]string{
		"length":                "max output",
		"content_filter":        "content filter",
		"repetition_truncation": "repetition",
	}
	for reason, fragment := range loud {
		msg, ok := finishReasonMessage(&provider.Usage{FinishReason: reason})
		if !ok || !strings.Contains(msg, fragment) {
			t.Errorf("finish_reason=%q: got (%q, %v), want fragment %q", reason, msg, ok, fragment)
		}
	}
}

// --- parallel-dispatch tests ---

// fakeTool is a minimal Tool stand-in for dispatch tests; ReadOnly is
// configurable and Execute sleeps a fixed duration so we can measure
// serial vs parallel behaviour by wall-clock.
type fakeTool struct {
	name     string
	readOnly bool
	delay    time.Duration
	err      error
	calls    *int32 // shared counter to assert all dispatched
}

func (f fakeTool) Name() string            { return f.name }
func (f fakeTool) Description() string     { return "" }
func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) ReadOnly() bool          { return f.readOnly }
func (f fakeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.name + " done", nil
}

func TestCanParalleliseAllReadOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ro1", readOnly: true})
	reg.Add(fakeTool{name: "ro2", readOnly: true})
	calls := []provider.ToolCall{{Name: "ro1"}, {Name: "ro2"}}
	if !canParallelise(reg, calls) {
		t.Error("all-readonly batch should be parallelisable")
	}
}

// TestCanParalleliseMixedSerial verifies that a single write in a batch flips
// the whole batch back to serial, so read-after-write hazards can't reorder.
func TestCanParalleliseMixedSerial(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ro", readOnly: true})
	reg.Add(fakeTool{name: "rw", readOnly: false})
	calls := []provider.ToolCall{{Name: "ro"}, {Name: "rw"}, {Name: "ro"}}
	if canParallelise(reg, calls) {
		t.Error("mixed batch must be sequential")
	}
}

// TestCanParalleliseUnknownToolSerial keeps unknown-tool errors deterministic
// by forcing the batch through the sequential path.
func TestCanParalleliseUnknownToolSerial(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ro", readOnly: true})
	calls := []provider.ToolCall{{Name: "ro"}, {Name: "vanished"}}
	if canParallelise(reg, calls) {
		t.Error("unknown tool should force sequential dispatch")
	}
}

func TestCanParalleliseCompleteStepSerial(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "complete_step", readOnly: true})

	calls := []provider.ToolCall{{Name: "read_file"}, {Name: "complete_step"}}
	if canParallelise(reg, calls) {
		t.Error("complete_step batches must stay sequential so receipts are ordered")
	}
}

// TestExecuteBatchParallelReadOnly checks that three 80ms read-only calls
// complete in well under 3×80ms — the wall-clock proof of true parallelism.
func TestExecuteBatchParallelReadOnly(t *testing.T) {
	const delay = 80 * time.Millisecond
	calls := int32(0)
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "a", readOnly: true, delay: delay, calls: &calls})
	reg.Add(fakeTool{name: "b", readOnly: true, delay: delay, calls: &calls})
	reg.Add(fakeTool{name: "c", readOnly: true, delay: delay, calls: &calls})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	start := time.Now()
	results := a.executeBatch(context.Background(), []provider.ToolCall{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	elapsed := time.Since(start)

	if calls != 3 {
		t.Errorf("dispatched %d calls, want 3", calls)
	}
	if len(results) != 3 || results[0] != "a done" || results[1] != "b done" || results[2] != "c done" {
		t.Errorf("results out of order or wrong: %v", results)
	}
	// Allow generous slack for CI; even 2x serial would prove we got parallelism.
	if elapsed >= 2*delay {
		t.Errorf("read-only batch took %v (>= %v) — not parallel", elapsed, 2*delay)
	}
}

// TestExecuteBatchSerialOnWrite ensures a single write turn forces total
// serial time even though the other calls would otherwise parallelise.
func TestExecuteBatchSerialOnWrite(t *testing.T) {
	const delay = 40 * time.Millisecond
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ro", readOnly: true, delay: delay})
	reg.Add(fakeTool{name: "rw", readOnly: false, delay: delay})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	start := time.Now()
	a.executeBatch(context.Background(), []provider.ToolCall{{Name: "ro"}, {Name: "rw"}, {Name: "ro"}})
	elapsed := time.Since(start)

	// Three calls of `delay` in serial ≈ 3*delay; permit some slack.
	if elapsed < 2*delay {
		t.Errorf("mixed batch took only %v — looks like it ran in parallel", elapsed)
	}
}

func TestExecuteBatchFeedsReceiptsToCompleteStep(t *testing.T) {
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(completeStep)
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	results := a.executeBatch(context.Background(), []provider.ToolCall{
		{Name: "bash", Arguments: `{"command":"go test ./internal/..."}`},
		{Name: "complete_step", Arguments: `{
			"step":"Run checks",
			"result":"checks passed",
			"evidence":[{"kind":"verification","summary":"tests passed","command":"go test ./internal/..."}]
		}`},
	})

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if !strings.Contains(results[1], "host-verified 1") {
		t.Fatalf("complete_step did not see bash receipt: %q", results[1])
	}
}

func TestExecuteOneFailedReceiptDoesNotVerify(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, err: errors.New("boom")})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
	if out.errMsg == "" {
		t.Fatal("failing fake tool should return an error outcome")
	}
	if a.evidence.HasSuccessfulCommand("go test ./...") {
		t.Fatal("failed bash receipt must not verify")
	}
}

func TestRunResetsEvidenceLedger(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "done"}}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
	if !a.evidence.HasSuccessfulCommand("go test ./...") {
		t.Fatal("setup failed to record evidence")
	}

	if err := a.Run(context.Background(), "next turn"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.evidence.HasSuccessfulCommand("go test ./...") {
		t.Fatal("new user turn should not inherit previous receipts")
	}
}
