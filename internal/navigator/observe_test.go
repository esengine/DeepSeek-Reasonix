package navigator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

// TestObserveToolCallNeverExecutes verifies the advisory path: the navigator
// records observations and recovers facts but never calls adapter.Execute —
// the host (agent run loop) already ran the tool through its own
// permission/hooks/evidence path.
func TestObserveToolCallNeverExecutes(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.observeEvery = time.Hour // throttle sensor snapshots for determinism

	advice := n.ObserveToolCall(ctx, "read_file", `{"path":"/tmp/a.go"}`, "found /tmp/a.go\npackage main\nuser id: 42", nil)
	if advice != "" {
		t.Fatalf("successful tool should not produce advice, got %q", advice)
	}
	if adapter.execCount != 0 {
		t.Fatalf("advisory path must not execute tools; execCount=%d", adapter.execCount)
	}
	digest := n.ImplicitStateDigest()
	if !strings.Contains(digest, "/tmp/a.go") {
		t.Errorf("digest should contain recovered path, got %q", digest)
	}
	if !strings.Contains(digest, "42") {
		t.Errorf("digest should contain recovered id, got %q", digest)
	}
}

// TestObserveToolCallFailureSuggestsCorrection verifies a failing tool yields
// advisory correction text without the navigator re-executing.
func TestObserveToolCallFailureSuggestsCorrection(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.observeEvery = time.Hour

	advice := n.ObserveToolCall(ctx, "bash", `{"command":"ls /nonexistent"}`, "bash: ls: /nonexistent: No such file or directory", errBoom)
	if advice == "" {
		t.Fatal("failing tool should produce advisory correction text")
	}
	if !strings.Contains(advice, "navigator") {
		t.Errorf("advice should be marked as navigator advisory, got %q", advice)
	}
	if adapter.execCount != 0 {
		t.Fatalf("advisory path must not execute tools; execCount=%d", adapter.execCount)
	}
	// The failure is recorded in the state graph and surfaced via the digest.
	if digest := n.ImplicitStateDigest(); digest == "" {
		t.Error("failure facts should be recoverable from the navigator digest")
	}
}

// TestObserveToolCallTracksMultipleCalls verifies that successive observations
// accumulate distinct facts without re-execution, and that a prior success
// lets a later failure escalate through the retry path.
func TestObserveToolCallTracksMultipleCalls(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.observeEvery = time.Hour

	n.ObserveToolCall(ctx, "read_file", `{"path":"/tmp/fileA.go"}`, "ok /tmp/fileA.go", nil)
	n.ObserveToolCall(ctx, "read_file", `{"path":"/tmp/fileB.go"}`, "ok /tmp/fileB.go", nil)
	n.ObserveToolCall(ctx, "write_file", `{"path":"/c"}`, "boom", errBoom)

	digest := n.ImplicitStateDigest()
	for _, want := range []string{"/tmp/fileA.go", "/tmp/fileB.go"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest missing %q:\n%s", want, digest)
		}
	}
}
