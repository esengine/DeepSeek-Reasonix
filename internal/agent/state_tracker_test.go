package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestStateTrackerBeforeAfterToolCallPairs verifies that BeforeToolCall and
// AfterToolCall correctly pair via the token, and that the episodic entry is
// recorded with the right tool name and result hint.
func TestStateTrackerBeforeAfterToolCallPairs(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "read_file",
		Arguments: `{"path":"/some/file.go"}`,
	}
	token := tracker.BeforeToolCall(ctx, call)
	if token.seq == 0 {
		t.Fatal("BeforeToolCall returned zero-seq token")
	}

	result := "package main\n\nfunc main() {}\n// path: /some/file.go"
	tracker.AfterToolCall(ctx, token, result, nil)

	episodes := tracker.RecentEpisodes()
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episodic entry, got %d", len(episodes))
	}
	ep := episodes[0]
	if ep.ToolName != "read_file" {
		t.Errorf("ToolName = %q, want read_file", ep.ToolName)
	}
	if !ep.Success {
		t.Error("expected Success=true for nil error")
	}
	if !strings.Contains(ep.ArgsDigest, "path=/some/file.go") {
		t.Errorf("ArgsDigest should contain path, got: %s", ep.ArgsDigest)
	}
	t.Logf("✓ Episodic entry: tool=%s args=%s success=%v implicit=%v", ep.ToolName, ep.ArgsDigest, ep.Success, ep.Implicit)
}

// TestStateTrackerExtractsImplicitPaths verifies that file paths in tool
// results are extracted as implicit state. This is the core OSWorld 2.0
// defense: paths recovered from tool output are the most common implicit
// state lost to compaction.
func TestStateTrackerExtractsImplicitPaths(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "grep",
		Arguments: `{"pattern":"TODO"}`,
	}
	token := tracker.BeforeToolCall(ctx, call)

	// Result contains multiple file paths the agent should remember.
	result := "Found 3 matches:\n/some/path/main.go:42: TODO\n/another/pkg/util.go:15: TODO\n/home/user/config.yaml:1: TODO"
	tracker.AfterToolCall(ctx, token, result, nil)

	snapshot := tracker.SnapshotImplicitState()
	if snapshot == "" {
		t.Fatal("expected non-empty implicit state snapshot")
	}
	if !strings.Contains(snapshot, "/some/path/main.go") {
		t.Errorf("snapshot should contain /some/path/main.go, got: %s", snapshot)
	}
	if !strings.Contains(snapshot, "/another/pkg/util.go") {
		t.Errorf("snapshot should contain /another/pkg/util.go, got: %s", snapshot)
	}
	t.Logf("✓ Implicit state snapshot:\n%s", snapshot)
}

// TestStateTrackerExtractsIDs verifies that IDs in tool results are extracted
// as implicit state. IDs are the second most common implicit state lost to
// compaction (session IDs, entity IDs, PR numbers).
func TestStateTrackerExtractsIDs(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "api_call",
		Arguments: `{}`,
	}
	token := tracker.BeforeToolCall(ctx, call)

	result := `{"id": "pr-7577", "status": "open"}`
	tracker.AfterToolCall(ctx, token, result, nil)

	snapshot := tracker.SnapshotImplicitState()
	if !strings.Contains(snapshot, "pr-7577") {
		t.Errorf("snapshot should contain pr-7577, got: %s", snapshot)
	}
	t.Logf("✓ ID extracted: pr-7577")
}

// TestStateTrackerErrorRecordsImplicitState verifies that errors are recorded
// as implicit state. Error messages often reveal paths, IDs, and config values
// the agent never stated explicitly ("file not found: /path/to/X").
func TestStateTrackerErrorRecordsImplicitState(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "read_file",
		Arguments: `{"path":"/missing/file.go"}`,
	}
	token := tracker.BeforeToolCall(ctx, call)

	tracker.AfterToolCall(ctx, token, "error: file not found: /missing/file.go", nil)

	episodes := tracker.RecentEpisodes()
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	// The error path should be extracted as implicit state.
	snapshot := tracker.SnapshotImplicitState()
	if !strings.Contains(snapshot, "/missing/file.go") {
		t.Errorf("snapshot should contain the error path, got: %s", snapshot)
	}
	t.Logf("✓ Error path extracted as implicit state")
}

// TestStateTrackerEpisodicWindowEviction verifies that the sliding window
// evicts old entries when the capacity is exceeded. This ensures the tracker
// does not grow unboundedly across long sessions.
func TestStateTrackerEpisodicWindowEviction(t *testing.T) {
	tracker := NewDefaultStateTracker(3, nil) // small window for testing
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		call := provider.ToolCall{
			ID:        "call-" + string(rune('1'+i)),
			Name:      "tool",
			Arguments: `{}`,
		}
		token := tracker.BeforeToolCall(ctx, call)
		tracker.AfterToolCall(ctx, token, "result", nil)
	}

	episodes := tracker.RecentEpisodes()
	if len(episodes) != 3 {
		t.Fatalf("expected 3 episodes (window capacity), got %d", len(episodes))
	}
	t.Logf("✓ Episodic window evicted correctly: %d entries", len(episodes))
}

// TestStateTrackerResetClearsAll verifies that Reset clears all three layers.
func TestStateTrackerResetClearsAll(t *testing.T) {
	tracker := NewDefaultStateTracker(0, nil)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "read_file",
		Arguments: `{"path":"/some/file.go"}`,
	}
	token := tracker.BeforeToolCall(ctx, call)
	tracker.AfterToolCall(ctx, token, "/some/file.go content", nil)

	if len(tracker.RecentEpisodes()) != 1 {
		t.Fatal("expected 1 episode before Reset")
	}

	tracker.Reset()

	if len(tracker.RecentEpisodes()) != 0 {
		t.Errorf("expected 0 episodes after Reset, got %d", len(tracker.RecentEpisodes()))
	}
	if tracker.SnapshotImplicitState() != "" {
		t.Error("expected empty implicit state after Reset")
	}
	t.Logf("✓ Reset cleared all state")
}

// TestStateTrackerDiagnosticEvent verifies that the tracker emits a diagnostic
// event when implicit facts are extracted, so the user can see state being
// captured at runtime.
func TestStateTrackerDiagnosticEvent(t *testing.T) {
	var emitted []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			emitted = append(emitted, e)
		}
	})

	tracker := NewDefaultStateTracker(0, sink)
	ctx := context.Background()

	call := provider.ToolCall{
		ID:        "call-1",
		Name:      "read_file",
		Arguments: `{"path":"/some/file.go"}`,
	}
	token := tracker.BeforeToolCall(ctx, call)
	tracker.AfterToolCall(ctx, token, "content with /another/path.go reference", nil)

	if len(emitted) == 0 {
		t.Fatal("expected at least one diagnostic event for implicit state capture")
	}
	found := false
	for _, e := range emitted {
		if strings.Contains(e.Text, "implicit state captured") {
			found = true
			t.Logf("✓ Diagnostic event: %s", e.Text)
			break
		}
	}
	if !found {
		t.Error("no 'implicit state captured' event found")
	}
}

// TestStateTrackerConcurrentCalls verifies that the tracker is safe for
// concurrent use — the run loop is single-writer, but Snapshot/RecentEpisodes
// may be called from a status line reader goroutine.
func TestStateTrackerConcurrentCalls(t *testing.T) {
	tracker := NewDefaultStateTracker(50, nil)
	ctx := context.Background()

	// Simulate concurrent tool calls.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			call := provider.ToolCall{
				ID:        "call-" + string(rune('a'+i)),
				Name:      "tool",
				Arguments: `{}`,
			}
			token := tracker.BeforeToolCall(ctx, call)
			tracker.AfterToolCall(ctx, token, "result", nil)
		}
	}()

	// Reader goroutine: snapshot while writer is active.
	for i := 0; i < 10; i++ {
		_ = tracker.SnapshotImplicitState()
		_ = tracker.RecentEpisodes()
	}

	<-done
	episodes := tracker.RecentEpisodes()
	if len(episodes) != 20 {
		t.Errorf("expected 20 episodes, got %d", len(episodes))
	}
	t.Logf("✓ Concurrent calls safe: %d episodes recorded", len(episodes))
}
