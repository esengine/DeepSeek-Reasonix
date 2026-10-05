package delegation

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
)

// A repair inside a delegated run has to be told apart from one in the parent's
// own loop: "the child kept working" and "the host pulled the child back" are
// different answers, and the producers run where neither id is in hand. The
// attribution is asserted through the stack a child actually emits into, since
// that stack is where audits were being dropped entirely.
func TestProtocolRecoveryCarriesTheChildItHappenedIn(t *testing.T) {
	parent := &recoverySink{}
	tracker := newSubagentProgressTracker(context.Background(), subSinkFor("fleet-2", parent))
	defer tracker.finish(nil, nil)

	event.RecordProtocolRecovery(tracker.wrap(), event.ProtocolRecoveryAudit{
		Kind: event.ProtocolRecoveryMissingReasoningDetected,
	})
	event.RecordProtocolRecovery(parent, event.ProtocolRecoveryAudit{
		Kind: event.ProtocolRecoveryMissingReasoningDetected,
	})

	if len(parent.audits) != 2 {
		t.Fatalf("recovery audits = %d, want the child's and the parent's", len(parent.audits))
	}
	if got := parent.audits[0].ChildID; got != "fleet-2" {
		t.Errorf("child recovery ChildID = %q, want the delegated run's id", got)
	}
	if got := parent.audits[1].ChildID; got != "" {
		t.Errorf("parent recovery ChildID = %q, want empty for the parent's own loop", got)
	}
}
