package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Automatic prune/snip projections are gone. These APIs remain as no-ops so
// older call sites do not panic, but they never rewrite the model view.
func TestPruneAndSnipAreNoOps(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "read_file", Content: strings.Repeat("x", 8000)},
		{Role: provider.RoleUser, Content: "next"},
	}}
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 100_000, RecentKeep: 2}, event.Discard)
	st, err := a.PruneStaleToolResults()
	if err != nil {
		t.Fatal(err)
	}
	if st.Results != 0 {
		t.Fatalf("prune results = %d, want 0", st.Results)
	}
	st, err = a.SnipStaleToolResults()
	if err != nil {
		t.Fatal(err)
	}
	if st.Results != 0 {
		t.Fatalf("snip results = %d, want 0", st.Results)
	}
	if got := a.currentProjectionVersion(); got != 0 {
		t.Fatalf("projection version = %d, want 0", got)
	}
	for _, m := range sess.Snapshot() {
		if m.Role == provider.RoleTool && m.Content != strings.Repeat("x", 8000) {
			t.Fatal("canonical tool result was rewritten")
		}
	}
}

func TestBelowThresholdSamplingKeepsBoundedToolContentWithoutProjection(t *testing.T) {
	full := strings.Repeat("完整结果", 10_000)
	bounded := "legacy bounded result"
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "read"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: bounded, RawContent: full},
	}}
	a := New(&countingProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{ContextWindow: 1_000_000}, event.Discard)
	req, err := a.buildSamplingRequest(context.Background(), CompactionTriggerPressure)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.req.Messages[3].Content; got != bounded {
		t.Fatalf("provider tool result = %q, want bounded content", got)
	}
	if a.currentProjectionVersion() != 0 {
		t.Fatalf("below-threshold request installed projection version %d", a.currentProjectionVersion())
	}
	stored := sess.Snapshot()[3]
	if stored.Content != bounded || stored.RawContent != full {
		t.Fatal("request projection mutated compatibility storage")
	}
}

func TestPruneSurvivesSummaryFailureAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	bigTool := strings.Repeat("🧪", 12_000)
	bigWork := strings.Repeat("assistant work ", 12_000)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: bigTool},
		{Role: provider.RoleAssistant, Content: bigWork},
		{Role: provider.RoleUser, Content: "tail"},
	}}
	a := New(&fakeProvider{streamErr: errors.New("summary down")}, tool.NewRegistry(), sess, Options{
		ContextWindow: 60_000, CompactRatio: 0.50, SessionPath: path,
	}, event.Discard)
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatalf("pressure below hard ceiling should continue from prune: %v", err)
	}
	if a.currentProjectionVersion() != 1 || countToolResultsContaining(a.modelVisibleMessages(), toolPruneMarker) != 1 {
		t.Fatalf("prune did not survive summary failure: version=%d view=%+v", a.currentProjectionVersion(), a.modelVisibleMessages())
	}

	restarted := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 60_000, CompactRatio: 0.50, SessionPath: path}, event.Discard)
	if restarted.currentProjectionVersion() != 1 || countToolResultsContaining(restarted.modelVisibleMessages(), toolPruneMarker) != 1 {
		t.Fatalf("restart lost prune projection: version=%d view=%+v", restarted.currentProjectionVersion(), restarted.modelVisibleMessages())
	}
}

func TestPruneSidecarWriteFailureRollsBackWithoutAppliedReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	bigTool := strings.Repeat("界", toolPruneThresholdRunes+1)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "read"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: bigTool},
	}}
	appliedEvents := 0
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil && e.Maintenance.Status == "applied" {
			appliedEvents++
		}
	})
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 20_000, SessionPath: path}, sink)
	// A directory at the sidecar destination makes atomic publication fail
	// without relying on platform-specific permission behavior.
	if err := os.Mkdir(ContextStatePath(path), 0o755); err != nil {
		t.Fatal(err)
	}

	a.sess.compactionRunMu.Lock()
	advanced, err := a.pruneToolResultsToProjectionLocked(CompactionTriggerPressure)
	a.sess.compactionRunMu.Unlock()
	if err == nil {
		t.Fatal("prune unexpectedly succeeded with an unwritable sidecar destination")
	}
	if advanced {
		t.Fatal("failed prune reported projection progress")
	}
	if got := a.currentProjectionVersion(); got != 0 {
		t.Fatalf("projection version = %d, want rollback to 0", got)
	}
	if a.sess.compactionState.LastReceipt != nil {
		t.Fatalf("failed prune published in-memory receipt: %+v", a.sess.compactionState.LastReceipt)
	}
	if appliedEvents != 0 {
		t.Fatalf("applied maintenance events = %d, want 0", appliedEvents)
	}
	if got := sess.Snapshot()[3].Content; got != bigTool {
		t.Fatal("failed prune modified canonical tool content")
	}
}

// At the compact_ratio trigger, maintenance persists a prune projection first.
// If that projection clears pressure, no paid summary request is made.
func TestMaintenancePrunesBeforeSummaryAndStopsWhenPressureClears(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}
	for i := range 8 {
		id := "t" + string(rune('a'+i))
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: big},
		)
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleUser, Content: "tail"},
		provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	)
	prov := &countingProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), &Session{Messages: msgs}, Options{
		ContextWindow: 30_000, CompactRatio: 0.5, RecentKeep: 2,
	}, event.Discard)
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	if a.currentProjectionVersion() != 1 {
		t.Fatalf("projection version = %d, want 1", a.currentProjectionVersion())
	}
	if len(prov.got) != 0 {
		t.Fatalf("summarizer calls = %d, want 0", len(prov.got))
	}
	visible := a.modelVisibleMessages()
	if got := countToolResultsContaining(visible, toolPruneMarker); got != 8 {
		t.Fatalf("pruned tool results = %d, want 8", got)
	}
	if got := a.sess.compactionState.LastReceipt.Action; got != "prune" {
		t.Fatalf("receipt action = %q, want prune", got)
	}
	for _, msg := range a.sess.conversation.Snapshot() {
		if msg.Role == provider.RoleTool && msg.Content != big {
			t.Fatal("canonical tool result was modified")
		}
	}
}

func countToolResultsContaining(msgs []provider.Message, marker string) int {
	n := 0
	for _, msg := range msgs {
		if msg.Role == provider.RoleTool && strings.Contains(msg.Content, marker) {
			n++
		}
	}
	return n
}

func TestSnipStrategyStillBuildsBoundedCompatibilityContent(t *testing.T) {
	a := &Agent{svc: agentServices{tools: tool.NewRegistry()}}
	s := a.snipStrategyFor("read_file")
	if s.head <= 0 || s.tail <= 0 {
		t.Fatalf("snip strategy for read_file = %+v", s)
	}
	body, notice := truncateToolOutputFor(strings.Repeat("x", maxToolOutputBytes+100), "read_file", "call-1")
	if notice == "" || !strings.Contains(body, "call_id=call-1") {
		t.Fatalf("first-visible truncation missing marker: notice=%q body=%.200q", notice, body)
	}
	if len(body) > maxToolOutputBytes+200 {
		t.Fatalf("bounded body still oversized: %d", len(body))
	}
}

func TestModelInputMessagesKeepsBoundedToolContent(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "display user", RawContent: "raw user"},
		{Role: provider.RoleTool, Content: "bounded", RawContent: "complete tool result", ToolCallID: "call-1"},
	}

	got := modelInputMessages(msgs)
	if got[0].Content != "display user" {
		t.Fatalf("user content = %q, want display form", got[0].Content)
	}
	if got[1].Content != "bounded" {
		t.Fatalf("tool content = %q, want bounded Content", got[1].Content)
	}
	if got[1].RawContent != "" {
		t.Fatalf("provider-bound RawContent = %q, want empty", got[1].RawContent)
	}
	if msgs[1].Content != "bounded" || msgs[1].RawContent != "complete tool result" {
		t.Fatalf("stored message was mutated: %+v", msgs[1])
	}
}

func TestPruneToolResultUsesUnicodeCodePoints(t *testing.T) {
	head := strings.Repeat("界", toolPruneHeadRunes)
	middle := strings.Repeat("🧪", toolPruneThresholdRunes-toolPruneHeadRunes-toolPruneTailRunes+1)
	tail := strings.Repeat("尾", toolPruneTailRunes)
	original := head + middle + tail

	got, changed := pruneToolResultContent(original)
	if !changed {
		t.Fatal("oversized tool result was not pruned")
	}
	want := head + toolPruneMarker + tail
	if got != want {
		t.Fatalf("pruned content mismatch: got runes=%d want runes=%d", len([]rune(got)), len([]rune(want)))
	}
	if _, changed := pruneToolResultContent(strings.Repeat("🧪", toolPruneThresholdRunes)); changed {
		t.Fatal("tool result at threshold must remain intact")
	}
}

// Pruning must allocate for the retained head/marker/tail only. Converting the
// complete input to []rune makes a large ASCII tool result consume roughly
// four additional bytes per source byte exactly when maintenance is trying to
// recover from context pressure.
func TestPruneToolResultAllocationIsBoundedByRetainedContent(t *testing.T) {
	const inputBytes = 256 << 10
	large := strings.Repeat("x", inputBytes)
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			got, changed := pruneToolResultContent(large)
			if !changed || len(got) == 0 {
				b.Fatal("large tool result was not pruned")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > 64<<10 {
		t.Fatalf("prune allocated %d bytes/op for a %d-byte input; want allocation bounded by retained content", got, inputBytes)
	}
}

func TestPrunePreservesToolEnvelopeMetadata(t *testing.T) {
	exit := 7
	original := provider.Message{
		Role: provider.RoleTool, Name: "bash", ToolCallID: "call-7",
		Content: strings.Repeat("🧪", toolPruneThresholdRunes+1),
		Images:  []string{"data:image/png;base64,AA=="}, CreatedAt: 77,
		ToolExecution: &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &exit},
	}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-7", Name: "bash", Arguments: `{}`}}},
		original,
	}}
	a := New(nil, tool.NewRegistry(), sess, Options{}, event.Discard)
	a.sess.compactionRunMu.Lock()
	advanced, err := a.pruneToolResultsToProjectionLocked(CompactionTriggerPressure)
	a.sess.compactionRunMu.Unlock()
	if err != nil || !advanced {
		t.Fatalf("prune advanced=%v err=%v", advanced, err)
	}
	got := a.sess.compactionState.Projection.Messages[2]
	if got.Role != original.Role || got.Name != original.Name || got.ToolCallID != original.ToolCallID || got.CreatedAt != original.CreatedAt {
		t.Fatalf("tool envelope changed: got=%+v want=%+v", got, original)
	}
	if len(got.Images) != 1 || got.Images[0] != original.Images[0] || got.ToolExecution != original.ToolExecution {
		t.Fatalf("tool metadata changed: got=%+v want=%+v", got, original)
	}
	if !strings.Contains(got.Content, toolPruneMarker) {
		t.Fatalf("tool body was not pruned: runes=%d", len([]rune(got.Content)))
	}
}
