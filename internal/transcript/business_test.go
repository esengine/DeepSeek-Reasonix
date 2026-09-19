package transcript

import (
	"fmt"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/turnevent"
)

func businessFrame(t *testing.T, p *Projection, covered uint64, e event.Event) {
	t.Helper()
	wire := eventwire.ToWire(e)
	status := event.TurnInProgress
	if e.Kind == event.TurnDone {
		status = event.TurnCompleted
	}
	if err := p.ApplyFrame(turnevent.Envelope{SessionID: testIdentity.SessionID, RuntimeEpoch: testIdentity.RuntimeEpoch, TurnID: "turn", Kind: wire.Kind, Status: status, Event: wire}, covered); err != nil {
		t.Fatal(err)
	}
}

func TestBusinessSettlementUpdatesStreamingRowWithoutDuplicateOrPendingState(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	businessFrame(t, p, 0, event.Event{Kind: event.StreamAttempt, MessageID: "answer", AttemptID: "answer", StreamAttempt: event.StreamAttemptInfo{ID: "answer", Action: event.StreamAttemptBegin}})
	businessFrame(t, p, 0, event.Event{Kind: event.Text, MessageID: "answer", AttemptID: "answer", Text: "partial"})
	p.AcceptBusiness([]Message{{RecordID: "m:answer", MessageID: "answer", Role: "assistant", Content: "complete answer", Reasoning: "complete thinking"}}, 1, "turn", false)
	businessFrame(t, p, 1, event.Event{Kind: event.Message, MessageID: "answer", AttemptID: "answer", Text: "complete answer", Reasoning: "complete thinking"})
	businessFrame(t, p, 1, event.Event{Kind: event.StreamAttempt, MessageID: "answer", AttemptID: "answer", StreamAttempt: event.StreamAttemptInfo{ID: "answer", Action: event.StreamAttemptCommit}})
	businessFrame(t, p, 1, event.Event{Kind: event.TurnDone})
	cut := snapshot(t, p)
	if len(cut.Records) != 1 {
		t.Fatalf("settlement duplicated streaming node: records=%d", len(cut.Records))
	}
	message := cut.Records[0].Message
	if message.Content != "complete answer" || message.Reasoning != "complete thinking" || message.Pending {
		t.Fatalf("settled content or pending state is wrong: %+v", message)
	}
	if len(cut.ActiveAttempts) != 0 || len(cut.ActiveRecords) != 0 {
		t.Fatalf("settlement retains active state: attempts=%d records=%d", len(cut.ActiveAttempts), len(cut.ActiveRecords))
	}
}

func TestBusinessRowsWithoutCanonicalIdentityReceiveDistinctViewIdentity(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := p.Follow(t.Context(), FollowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	p.AcceptBusiness([]Message{{Role: "notice", Content: "first"}, {Role: "notice", Content: "second"}}, 1, "turn", false)
	cut := snapshot(t, p)
	if len(cut.Records) != 2 || cut.Records[0].ID == "" || cut.Records[0].ID == cut.Records[1].ID {
		t.Fatalf("business identity allocation = %+v", cut.Records)
	}
	suffix := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
	if len(suffix.Changes) != 1 || len(suffix.Changes[0].Records) != 2 || suffix.Changes[0].Records[0].RecordID == "" ||
		suffix.Changes[0].Records[0].RecordID == suffix.Changes[0].Records[1].RecordID {
		t.Fatalf("published business identities = %+v", suffix.Changes)
	}
}

func TestBusinessPublisherReleasesCompletedActiveRowsWithinHistoryBudget(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	businessFrame(t, p, 0, event.Event{Kind: event.StreamAttempt, MessageID: "old-answer", AttemptID: "old-answer", StreamAttempt: event.StreamAttemptInfo{ID: "old-answer", Action: event.StreamAttemptBegin}})
	businessFrame(t, p, 0, event.Event{Kind: event.Text, MessageID: "old-answer", Text: "old partial answer"})
	for i := range 120 {
		id := fmt.Sprintf("new-answer-%03d", i)
		p.AcceptBusiness([]Message{{RecordID: "m:" + id, MessageID: id, Role: "assistant", Content: id}}, uint64(i+1), fmt.Sprintf("turn-%d", i), false)
	}
	active := snapshot(t, p)
	retained := false
	for _, records := range [][]Record{active.Records, active.ActiveRecords} {
		for _, record := range records {
			retained = retained || record.Message.MessageID == "old-answer" && record.Message.Content == "old partial answer"
		}
	}
	if len(active.ActiveAttempts) != 1 || !retained {
		t.Fatal("appended history evicted the active assistant prefix")
	}
	// A terminal turn must release pending state even if its attempt end frame
	// was unavailable. No later user submission should be needed to reclaim
	// rows that were exempted from the resident budget while still active.
	businessFrame(t, p, 120, event.Event{Kind: event.TurnDone})
	cut := snapshot(t, p)
	if cut.TotalRecords > 96 || len(cut.ActiveRecords) != 0 || len(cut.ActiveAttempts) != 0 {
		t.Fatalf("completed rows escaped resident budget: total=%d activeRows=%d activeAttempts=%d", cut.TotalRecords, len(cut.ActiveRecords), len(cut.ActiveAttempts))
	}
	for _, record := range cut.Records {
		if record.Message.MessageID == "old-answer" || record.Message.Pending {
			t.Fatalf("completed active row was retained indefinitely: %+v", record.Message)
		}
	}
}
