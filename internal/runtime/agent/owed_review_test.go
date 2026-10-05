package agent

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

// resumedWithOwedReview is an agent a restart handed a checkpoint owing both
// reviews over auth/login.go, inside the delivery scope that incurred them.
func resumedWithOwedReview(t *testing.T) *Agent {
	t.Helper()
	a := New(&stepProvider{}, deliveryRegistry(), sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{ScopeID: "goal-auth", OwedReview: &evidence.OwedReview{
		Kinds: []evidence.ReviewKind{evidence.ReviewKindReview, evidence.ReviewKindSecurity},
		Paths: []string{"auth/login.go"},
	}})
	enterScope(a, "goal-auth")
	return a
}

// enterScope is the state beginRunTurn leaves a delivery turn in. Whether a
// restart reaches it with a carried review is shown through the real boot by
// TestEffectRestartKeepsTheReviewAHighRiskGoalOwes.
func enterScope(a *Agent, id string) {
	a.task.scopeID = id
	a.turn.deliveryScopeActive = true
}

// A carried review is settled by the reviews it names and only by them: one
// kind leaves the other owed, both clear the gate and the checkpoint.
func TestCarriedReviewIsSettledOnlyByCoveringReviews(t *testing.T) {
	a := resumedWithOwedReview(t)
	if msg := a.carriedReviewFailure(); !strings.Contains(msg, "require review with review_report") {
		t.Fatalf("carried failure = %q, want the review demanded", msg)
	}
	a.task.ledger.Record(grantedReviewReceipt(evidence.ReviewKindReview, `{"kind":"review","verdict":"pass","reviewed_paths":["auth/login.go"]}`))
	if msg := a.carriedReviewFailure(); !strings.Contains(msg, "security_review") {
		t.Fatalf("after review only: %q, want security_review still owed", msg)
	}
	a.task.ledger.Record(grantedReviewReceipt(evidence.ReviewKindSecurity, `{"kind":"security","verdict":"pass","reviewed_paths":["auth/login.go"]}`))
	if msg := a.carriedReviewFailure(); msg != "" {
		t.Fatalf("after both reviews: %q, want nothing owed", msg)
	}
	if owed := a.owedReview(a.task.checkpoint.OwedReview); owed != nil {
		t.Fatalf("checkpoint would still carry %+v", owed)
	}
}

// A review that did not cover the owed path settles nothing.
func TestCarriedReviewIgnoresAReviewOfOtherPaths(t *testing.T) {
	a := resumedWithOwedReview(t)
	a.task.ledger.Record(grantedReviewReceipt(evidence.ReviewKindReview, `{"kind":"review","verdict":"pass","reviewed_paths":["other.go"]}`))
	owed := a.owedReview(a.task.checkpoint.OwedReview)
	if owed == nil || !slices.Contains(owed.Kinds, evidence.ReviewKindReview) {
		t.Fatalf("owed = %+v, want review still owed", owed)
	}
}

// Outside the scope that incurred it, a carried review binds nothing.
func TestCarriedReviewIsScopedToItsDelivery(t *testing.T) {
	a := New(&stepProvider{}, deliveryRegistry(), sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{ScopeID: "goal-auth", OwedReview: &evidence.OwedReview{
		Kinds: []evidence.ReviewKind{evidence.ReviewKindReview}, Paths: []string{"auth/login.go"},
	}})
	enterScope(a, "goal-other")
	if msg := a.carriedReviewFailure(); msg != "" {
		t.Fatalf("another scope was held to it: %q", msg)
	}
}
