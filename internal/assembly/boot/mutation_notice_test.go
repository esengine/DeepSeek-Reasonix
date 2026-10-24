package boot

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/state/mutation"
)

func TestMutationNoticeProjectsTypedEndReason(t *testing.T) {
	for _, reason := range []string{"cancelled", "timeout", "conflict", "rejected"} {
		e, ok := mutationNotice(mutation.Notice{State: "abandoned", TransactionID: "txn", Reason: reason})
		if !ok {
			t.Fatal("dropped closure")
		}
		wire := eventwire.ToWire(e)
		if wire.WorkspaceLease.Reason != reason || strings.Contains(wire.Detail, "the turn was cancelled or timed out first") {
			t.Fatalf("misclassified closure: %+v", wire)
		}
	}
	for _, state := range []string{"released", "future"} {
		if _, ok := mutationNotice(mutation.Notice{State: state}); ok {
			t.Fatalf("invented wait for %s", state)
		}
	}
}

func TestCapacityNoticeDoesNotClaimFilesystemOwnership(t *testing.T) {
	for _, state := range []string{"waiting", "acquired", "abandoned"} {
		e, ok := mutationNotice(mutation.Notice{Effect: mutation.CapacityOnly, State: state, TransactionID: "model-txn"})
		if !ok || strings.Contains(e.Text, "write") || !strings.Contains(e.Text, "capacity") {
			t.Fatalf("wrong resource description: %+v", e)
		}
		wire := eventwire.ToWire(e)
		if wire.WorkspaceLease.ResourceKind != "capacity" || wire.WorkspaceLease.State != state || wire.WorkspaceLease.TransactionID != "model-txn" {
			t.Fatalf("missing capacity identity: %+v", wire)
		}
	}
}
