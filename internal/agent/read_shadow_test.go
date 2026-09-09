package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

func newShadowTestAgent(t *testing.T, enabled bool) (*Agent, *Session) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(envelopeReader{env: tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		Source:          tool.ReadResultSource{CanonicalPath: "/w/a.go", Snapshot: "ss2:v1"},
		Intent:          tool.ReadIntentInspect,
		DeliveredRanges: []tool.ReadRange{{Start: 0, End: 2000}},
		HasMore:         true,
	}})
	sess := NewSession("system")
	a := New(&userInputCaptureProvider{}, reg, sess, Options{}, event.Discard)
	a.reads.tasks = newReadTasks("test-session", 1)
	a.turn.readShadow = newReadShadowState(enabled)
	return a, sess
}

func observeOneRead(a *Agent) {
	a.storeBatchToolResult(context.Background(),
		provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		toolOutcome{output: "   1→a\n"},
	)
}

func TestReadShadowIsInertUnlessEnabled(t *testing.T) {
	a, _ := newShadowTestAgent(t, false)
	observeOneRead(a)
	if a.turn.readShadow.observed != 0 || a.turn.readShadow.coord != nil {
		t.Fatalf("disabled shadow must stay inert: %+v", a.turn.readShadow)
	}
}

func TestReadShadowRecordsTheCoordinatorVerdict(t *testing.T) {
	a, _ := newShadowTestAgent(t, true)
	observeOneRead(a)
	s := a.turn.readShadow
	if s.observed != 1 || s.byState[readcoord.StateSatisfied] != 1 {
		t.Fatalf("shadow = %+v, want one satisfied observation", s)
	}
	if s.disagreements != 0 {
		t.Fatalf("legacy has no pending read, so there is no disagreement: %+v", s)
	}
}

func TestReadShadowRecordsLegacyDisagreement(t *testing.T) {
	a, _ := newShadowTestAgent(t, true)
	a.turn.incompleteReads.addEntryLocked(&incompleteRead{key: "k", path: "/w/a.go"})
	observeOneRead(a)
	s := a.turn.readShadow
	if s.observed != 1 || s.disagreements != 1 {
		t.Fatalf("shadow = %+v, want one observation and one disagreement", s)
	}
}

func TestReadShadowDoesNotChangeStoredContent(t *testing.T) {
	a, sess := newShadowTestAgent(t, true)
	observeOneRead(a)
	stored := sess.Snapshot()
	if len(stored) == 0 || stored[len(stored)-1].Content != "   1→a\n" {
		t.Fatalf("shadow must not rewrite the provider-visible result: %+v", stored)
	}
}

// TestReadShadowNarrowsAnUnboundedFullRead pins the budget coupling: when the
// host cannot size a safe automatic read, a whole-file obligation becomes
// needs_scope instead of continuing on a guess.
func TestReadShadowNarrowsAnUnboundedFullRead(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(envelopeReader{env: tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		Source:          tool.ReadResultSource{CanonicalPath: "/w/a.go", Snapshot: "ss2:v1"},
		Intent:          tool.ReadIntentFull,
		DeliveredRanges: []tool.ReadRange{{Start: 0, End: 10}},
		HasMore:         true,
	}})
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{}, event.Discard)
	a.reads.tasks = newReadTasks("test-session", 1)
	a.turn.readShadow = newReadShadowState(true)
	a.storeBatchToolResult(context.Background(),
		provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go","intent":"full"}`},
		toolOutcome{output: "  1→a\n"},
	)

	snapshot := a.turn.readShadow.coord.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("obligations = %+v, want one", snapshot)
	}
	if snapshot[0].State != readcoord.StateNeedsScope {
		t.Fatalf("state = %s, want needs_scope without a known budget", snapshot[0].State)
	}
	if snapshot[0].Stop == nil || snapshot[0].Stop.Code != "unknown_window" {
		t.Fatalf("stop reason = %+v, want unknown_window", snapshot[0].Stop)
	}
}

type recordingSink struct{ events []event.Event }

func TestReadStatusEmitterPreservesZeroBasedRanges(t *testing.T) {
	sink := &recordingSink{}
	a := New(&userInputCaptureProvider{}, tool.NewRegistry(), NewSession("system"), Options{}, sink)
	a.emitReadStatus(readcoord.Transition{
		Key: "read", To: readcoord.StateNeedsMore,
		Covered: []tool.ReadRange{{Start: 0, End: 10}, {Start: 100, End: 110}},
		Missing: []tool.ReadRange{{Start: 10, End: 100}},
	}, tool.ReadResultEnvelope{Intent: tool.ReadIntentFull})
	frames := sink.readStatuses()
	if len(frames) != 1 {
		t.Fatalf("frames=%d", len(frames))
	}
	want := [][2]int{{0, 10}, {100, 110}}
	if len(frames[0].Covered) != 2 || frames[0].Covered[0] != want[0] || frames[0].Covered[1] != want[1] || len(frames[0].Missing) != 1 || frames[0].Missing[0] != [2]int{10, 100} {
		t.Fatalf("emitter shifted source coordinates: %+v", frames[0])
	}
}

func (s *recordingSink) Emit(e event.Event) { s.events = append(s.events, e) }

func (s *recordingSink) readStatuses() []*event.ReadStatusPayload {
	var out []*event.ReadStatusPayload
	for _, e := range s.events {
		if e.Kind == event.ReadStatus && e.ReadStatus != nil {
			out = append(out, e.ReadStatus)
		}
	}
	return out
}

// TestReadShadowEmitsOneUpsertedStatusPerRead pins the UI contract: every page
// of one logical read carries the same read id and an increasing sequence, so a
// frontend updates a single status card instead of appending a page per notice.
func TestReadShadowEmitsOneUpsertedStatusPerRead(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(envelopeReader{env: tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		Source:          tool.ReadResultSource{CanonicalPath: "/w/a.go", Snapshot: "ss2:v1"},
		Intent:          tool.ReadIntentRange,
		RequestedRange:  &tool.ReadRange{Start: 0, End: 40},
		DeliveredRanges: []tool.ReadRange{{Start: 0, End: 10}},
		HasMore:         true,
	}})
	sink := &recordingSink{}
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{}, sink)
	a.reads.tasks = newReadTasks("test-session", 1)
	a.turn.readShadow = newReadShadowState(true)

	for page := range 3 {
		a.storeBatchToolResult(context.Background(),
			provider.ToolCall{ID: "call-" + string(rune('a'+page)), Name: "read_file", Arguments: `{"path":"a.go","offset":0,"limit":10}`},
			toolOutcome{output: "  1→a\n", readTaskID: "ir-1"},
		)
	}

	statuses := sink.readStatuses()
	if len(statuses) != 3 {
		t.Fatalf("status events = %d, want one per page", len(statuses))
	}
	for i, status := range statuses {
		if status.ReadID != "ir-1" {
			t.Fatalf("status %d read id = %q, want ir-1 (upsert key)", i, status.ReadID)
		}
		if status.Path != "/w/a.go" || status.Intent != string(tool.ReadIntentRange) {
			t.Fatalf("status %d = %+v", i, status)
		}
	}
	if statuses[0].Sequence >= statuses[2].Sequence {
		t.Fatalf("sequences must advance: %+v", statuses)
	}
}

// TestReadContinuationKeepsAPivotForEveryStalledRead pins the advice set: two
// reads that stall in the same round must each receive their one strategy
// change, because the coordinator never offers a second one.
func TestReadContinuationKeepsAPivotForEveryStalledRead(t *testing.T) {
	reg := tool.NewRegistry()
	a := New(&userInputCaptureProvider{}, reg, NewSession("system"), Options{ContextWindow: 64_000}, event.Discard)
	a.reads.tasks = newReadTasks("test-session", 1)
	a.turn.readShadow = newReadShadowState(true)
	for _, id := range []string{"ir-0", "ir-1"} {
		env := tool.ReadResultEnvelope{
			ReadID:          id,
			Source:          tool.ReadResultSource{CanonicalPath: "/w/" + id + ".go", Snapshot: "ss2:v1"},
			Intent:          tool.ReadIntentFull,
			DeliveredRanges: []tool.ReadRange{{Start: 0, End: 10}},
			HasMore:         true,
			NextCursor: tool.EncodeReadCursor(tool.ReadCursor{
				ReadID: id, Path: "/w/" + id + ".go", Snapshot: "ss2:v1", NextStart: 10,
			}),
		}
		a.observeReadShadow(env, 0)
		a.reads.tasks.remember(id, env)
		a.observeReadShadow(env, 0)
		a.observeReadShadow(env, 0)
	}
	first, err := a.readContinuation(false)
	if err != nil {
		t.Fatalf("continuation: %v", err)
	}
	if !strings.Contains(first, "Change strategy") || !strings.Contains(first, "/w/ir-0.go") {
		t.Fatalf("the first stalled read lost its pivot advice: %q", first)
	}
	if _, owed := a.turn.readShadow.pivots["ir-1"]; !owed {
		t.Fatal("the second stalled read lost its owed pivot advice")
	}
}
