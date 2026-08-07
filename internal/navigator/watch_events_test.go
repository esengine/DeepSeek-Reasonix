package navigator

import (
	"context"
	"strings"
	"testing"
	"time"
)

// watchTestAdapter is a no-op HostAdapter for building a Navigator in tests.
type watchTestAdapter struct{}

func (watchTestAdapter) Execute(ctx context.Context, action HostAction) (HostResult, error) {
	return HostResult{Output: ""}, nil
}

func (watchTestAdapter) Permission(ctx context.Context, action HostAction) (bool, string) {
	return true, ""
}

func (watchTestAdapter) Emit(ctx context.Context, event HostEvent) {}

func (watchTestAdapter) InterfaceProbe(ctx context.Context) (string, error) { return "", nil }

func (watchTestAdapter) SnapshotEnv(ctx context.Context) (string, error) { return "", nil }

func TestPendingWatchEventsDrainsAndFormats(t *testing.T) {
	n := New(watchTestAdapter{}, Options{HistoryWindow: 50})
	if got := n.PendingWatchEvents(); len(got) != 0 {
		t.Fatalf("empty buffer should drain to nothing, got %v", got)
	}
	// Seed the correlator directly (as the background watch would via
	// SnapshotAll → Ingest).
	now := time.Now()
	n.sensor.correlator.Ingest([]SensorEvent{
		{Source: "filesystem", Kind: "modify", Subject: "a.go", At: now, Detail: "size 100→200"},
		{Source: "process", Kind: "appear", Subject: "python", At: now.Add(time.Millisecond)},
	})
	lines := n.PendingWatchEvents()
	if len(lines) != 1 {
		t.Fatalf("correlator batches the two events into one line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "[env correlated] batch") || !strings.Contains(lines[0], "2 events from 2 sources") {
		t.Errorf("line[0] = %q", lines[0])
	}
	// Drain is consuming: a second call returns nothing.
	if again := n.PendingWatchEvents(); len(again) != 0 {
		t.Errorf("second drain should be empty, got %v", again)
	}
}
