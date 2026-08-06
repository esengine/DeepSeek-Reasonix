package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/navigator"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// failingTestTool always fails, so tests can exercise the error path of the
// run loop (StateTracker error recording + Navigator advisory corrections).
type failingTestTool struct{}

func (failingTestTool) Name() string        { return "fail_tool" }
func (failingTestTool) Description() string { return "always fails (test)" }
func (failingTestTool) ReadOnly() bool      { return true }
func (failingTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (failingTestTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "fail_tool exploded", errors.New("boom")
}

// TestStateTrackerImplicitDedupAcrossCalls verifies review fix 4: the same
// fact recovered from two different tool calls is stored once (later wins),
// not appended twice.
func TestStateTrackerImplicitDedupAcrossCalls(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()
	call := provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"/x"}`}
	tk := tracker.BeforeToolCall(ctx, call)
	tracker.AfterToolCall(ctx, tk, "found /data/id.txt", nil)
	tk2 := tracker.BeforeToolCall(ctx, call)
	tracker.AfterToolCall(ctx, tk2, "found /data/id.txt again", nil)

	snap := tracker.SnapshotImplicitState()
	if strings.Count(snap, "/data/id.txt") != 1 {
		t.Fatalf("expected deduped fact, got:\n%s", snap)
	}
}

// TestStateTrackerImplicitCapacityCapped verifies review fix 4: the implicit
// fact set is bounded by maxImplicit (oldest evicted) so long tasks cannot
// grow it without limit.
func TestStateTrackerImplicitCapacityCapped(t *testing.T) {
	s := &defaultStateTracker{maxEpisodic: 20, maxImplicit: 3, factIx: map[string]int{}}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		call := provider.ToolCall{ID: "c", Name: "grep", Arguments: `{}`}
		tk := s.BeforeToolCall(ctx, call)
		s.AfterToolCall(ctx, tk, fmt.Sprintf("/data/fact%d.txt", i), nil)
	}
	snap := s.SnapshotImplicitState()
	if got := strings.Count(snap, "\n"); got != 2 {
		t.Fatalf("expected 3 capped facts (2 newlines), got %d newlines:\n%s", got, snap)
	}
	if strings.Contains(snap, "fact0") {
		t.Errorf("oldest facts should be evicted, got:\n%s", snap)
	}
	if !strings.Contains(snap, "fact4") {
		t.Errorf("newest fact should survive, got:\n%s", snap)
	}
}

// TestStateTrackerAfterToolCallRecordsError verifies review fix 3 wiring: the
// real per-call error is recorded (Success=false) and surfaces in the
// implicit-state snapshot instead of being swallowed as nil.
func TestStateTrackerAfterToolCallRecordsError(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()
	call := provider.ToolCall{ID: "c1", Name: "bash", Arguments: `{"command":"false"}`}
	tk := tracker.BeforeToolCall(ctx, call)
	tracker.AfterToolCall(ctx, tk, "bash: command failed", errors.New("exit status 1"))

	eps := tracker.RecentEpisodes()
	if len(eps) != 1 || eps[0].Success {
		t.Fatalf("expected one failed episodic entry, got %+v", eps)
	}
	snap := tracker.SnapshotImplicitState()
	if !strings.Contains(snap, "exit status 1") {
		t.Errorf("error fact should be captured, got %q", snap)
	}
}

// TestSummaryPromptSwitch verifies review fix 5: long_horizon controls which
// compaction prompt is used — 10 sections when on, legacy 7 when off.
func TestSummaryPromptSwitch(t *testing.T) {
	on := &Agent{longHorizon: true}
	off := &Agent{}
	if !strings.Contains(on.summaryPrompt(), "Hidden state & recovered facts") {
		t.Error("long-horizon prompt must include the hidden-state section")
	}
	if strings.Contains(off.summaryPrompt(), "Hidden state & recovered facts") {
		t.Error("standard prompt must NOT include the hidden-state section")
	}
	if got := strings.Count(off.summaryPrompt(), "## "); got != 7 {
		t.Errorf("standard prompt should have 7 sections, got %d", got)
	}
	if got := strings.Count(on.summaryPrompt(), "## "); got != 10 {
		t.Errorf("long-horizon prompt should have 10 sections, got %d", got)
	}
}

// TestExecuteBatchExposesPerCallErrors verifies review fix 3: executeBatch
// returns the real per-call error so the run loop can pass it to the
// StateTracker instead of hard-coding nil.
func TestExecuteBatchExposesPerCallErrors(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failingTestTool{})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	ctx := context.Background()
	batch := a.executeBatch(ctx, []provider.ToolCall{
		{ID: "c1", Name: "fail_tool", Arguments: `{}`},
	})
	if len(batch.errs) != 1 {
		t.Fatalf("expected 1 err slot, got %d", len(batch.errs))
	}
	if batch.errs[0] == nil {
		t.Error("failed tool must produce a non-nil err in batch.errs")
	}
	if batch.errs[0].Error() != "boom" {
		t.Errorf("err = %v, want boom", batch.errs[0])
	}
}

// TestRunLoopFeedsNavigatorAndTracker is the integration test for review
// fixes 1 and 3: a full Run with a failing tool must (a) pass the real error
// to the StateTracker, and (b) feed the Navigator through ObserveToolCall so
// its state graph recovers facts — proving the navigator is no longer dead
// code and the run loop still executes tools through the agent's own path.
func TestRunLoopFeedsNavigatorAndTracker(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failingTestTool{})
	sink := &recordSink{}
	nav := navigator.New(navigator.NewReasonixAdapter(reg, sink, navigator.ReasonixAdapterOptions{}), navigator.Options{HistoryWindow: 20})
	tracker := NewDefaultStateTracker(0, sink)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "fail_tool", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{
		StateTracker: tracker,
		Navigator:    nav,
		LongHorizon:  true,
	}, sink)
	if err := a.Run(context.Background(), "do it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// (a) StateTracker saw the failure (not nil).
	eps := tracker.RecentEpisodes()
	if len(eps) != 1 || eps[0].Success {
		t.Fatalf("StateTracker should record the failed call, got %+v", eps)
	}
	if digest := tracker.SnapshotImplicitState(); !strings.Contains(digest, "boom") {
		t.Errorf("StateTracker digest should include the error fact, got %q", digest)
	}
	// (b) Navigator was fed via ObserveToolCall and recovered facts.
	if digest := nav.ImplicitStateDigest(); !strings.Contains(digest, "boom") {
		t.Errorf("Navigator digest should include the error fact, got %q", digest)
	}
}
