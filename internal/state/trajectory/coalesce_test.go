package trajectory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

func recorderAt(t *testing.T, name string, sink event.Sink) (*Recorder, string) {
	t.Helper()
	path := filepath.Join(testenv.TempDir(t), name)
	tick := int64(0)
	clock := func() time.Time {
		tick++
		return time.UnixMilli(tick)
	}
	rec, err := New(sink, path, clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rec, path
}

// The increments a run emits by the hundred become one record each, and the
// only timestamp any reader consumes — the first — has to survive that.
func TestAdjacentDeltasMergeIntoOneRecord(t *testing.T) {
	sink := &capabilitySink{}
	rec, path := recorderAt(t, "merge.jsonl", sink)
	for _, s := range []string{"think", "ing ", "hard"} {
		rec.Emit(event.Event{Kind: event.Reasoning, Text: s})
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readRecords(t, path)
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.Event == nil || r.Event.Text != "thinking hard" {
		t.Fatalf("text = %q, want %q", r.Event.Text, "thinking hard")
	}
	if r.TS != 1 {
		t.Errorf("TS = %d, want the first increment's 1", r.TS)
	}
	if r.Deltas != 3 {
		t.Errorf("Deltas = %d, want 3", r.Deltas)
	}
	if r.EndTS != 3 {
		t.Errorf("EndTS = %d, want the last increment's 3", r.EndTS)
	}
	// Forwarding is untouched: the inner sink still sees every increment.
	if len(sink.events) != 3 {
		t.Errorf("forwarded %d events, want 3", len(sink.events))
	}
}

func TestDeltasOfDifferentKindsStayApart(t *testing.T) {
	rec, path := recorderAt(t, "kinds.jsonl", &capabilitySink{})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "why"})
	rec.Emit(event.Event{Kind: event.Text, Text: "answer"})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "more"})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := readRecords(t, path)
	if len(got) != 3 {
		t.Fatalf("records = %d, want 3", len(got))
	}
	for i, want := range []string{"why", "answer", "more"} {
		if got[i].Event.Text != want {
			t.Errorf("record %d text = %q, want %q", i, got[i].Event.Text, want)
		}
		if got[i].Deltas != 0 {
			t.Errorf("record %d merged %d, want a lone increment", i, got[i].Deltas)
		}
	}
}

// Merging must never reorder: whatever interrupts the run lands after it.
func TestNonDeltaEventFlushesTheRunInOrder(t *testing.T) {
	rec, path := recorderAt(t, "order.jsonl", &capabilitySink{})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "a"})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "b"})
	rec.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: "some_kind"})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "c"})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := readRecords(t, path)
	if len(got) != 3 {
		t.Fatalf("records = %d, want 3: %+v", len(got), got)
	}
	if got[0].Event == nil || got[0].Event.Text != "ab" {
		t.Errorf("first record = %+v, want the merged \"ab\"", got[0].Event)
	}
	if got[1].ProtocolRecovery != "some_kind" {
		t.Errorf("second record = %+v, want the recovery", got[1])
	}
	if got[2].Event == nil || got[2].Event.Text != "c" {
		t.Errorf("third record = %+v, want \"c\"", got[2].Event)
	}
	for i, r := range got {
		if r.Seq != uint64(i+1) {
			t.Errorf("record %d has seq %d, want %d", i, r.Seq, i+1)
		}
	}
}

// Only an event carrying nothing but its text may merge. This is the guard a
// field added to the wire later has to trip: without it the new payload would
// vanish into a neighbour's record instead of landing as its own.
func TestOnlyBareTextIncrementsMerge(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   eventwire.Event
		want bool
	}{
		{name: "bare reasoning", ev: eventwire.Event{Kind: "reasoning", Text: "x"}, want: true},
		{name: "bare text", ev: eventwire.Event{Kind: "text", Text: "x"}, want: true},
		{name: "carries a level", ev: eventwire.Event{Kind: "reasoning", Text: "x", Level: "warn"}},
		{name: "carries an error", ev: eventwire.Event{Kind: "text", Text: "x", Err: "boom"}},
		{name: "carries a tool", ev: eventwire.Event{Kind: "text", Text: "x", Tool: &eventwire.Tool{Name: "bash"}}},
		{name: "carries a phase", ev: eventwire.Event{Kind: "text", Text: "x", Phase: "working"}},
		{name: "no text", ev: eventwire.Event{Kind: "reasoning"}},
		{name: "another kind", ev: eventwire.Event{Kind: "message", Text: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := plainDelta(&tc.ev); got != tc.want {
				t.Fatalf("plainDelta = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergingIsBoundedSoAKillLosesLittle(t *testing.T) {
	rec, path := recorderAt(t, "cap.jsonl", &capabilitySink{})
	for range coalesceCap*2 + 5 {
		rec.Emit(event.Event{Kind: event.Reasoning, Text: "x"})
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := readRecords(t, path)
	if len(got) != 3 {
		t.Fatalf("records = %d, want 3 at a %d cap", len(got), coalesceCap)
	}
	total := 0
	for _, r := range got {
		total += len(r.Event.Text)
	}
	if total != coalesceCap*2+5 {
		t.Errorf("kept %d characters, want %d — merging must not drop any", total, coalesceCap*2+5)
	}
}

// The run in flight when a file is closed still has to reach the disk.
func TestCloseFlushesTheRunInFlight(t *testing.T) {
	rec, path := recorderAt(t, "close.jsonl", &capabilitySink{})
	rec.Emit(event.Event{Kind: event.Reasoning, Text: "unterminated"})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := readRecords(t, path)
	if len(got) != 1 || !strings.Contains(got[0].Event.Text, "unterminated") {
		t.Fatalf("records = %+v, want the pending increment flushed", got)
	}
}
