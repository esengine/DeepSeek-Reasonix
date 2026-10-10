package agent

import (
	"errors"
	"fmt"
	"testing"
)

// The reason used to be cut back out of the sentence at the sentinel's own
// wording. It now travels as a field, so rewording the sentinel cannot quietly
// turn a reason into the whole error.
func TestDeclineReasonSurvivesRewordingTheSentinel(t *testing.T) {
	err := rejectCheckpoint(NoopCandidateAboveTrigger, "candidate %d still at or above trigger %d", 900, 800)
	if !IsCompactionDeclined(err) {
		t.Fatal("a rejection did not read as one")
	}
	const want = "candidate 900 still at or above trigger 800"
	if got := CompactionDeclineReason(err); got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	// Wrapped further on the way up, the identity and the reason both survive.
	wrapped := fmt.Errorf("prepare context: %w", err)
	if !IsCompactionDeclined(wrapped) {
		t.Fatal("a wrapped rejection did not read as one")
	}
	if got := CompactionDeclineReason(wrapped); got != want {
		t.Fatalf("wrapped reason = %q, want %q", got, want)
	}
}

// Anything else keeps reporting itself, so a real failure is not rendered as a
// verdict with an empty reason.
func TestDeclineReasonLeavesOtherErrorsAlone(t *testing.T) {
	other := errors.New("disk went away")
	if IsCompactionDeclined(other) {
		t.Fatal("an unrelated error read as a decline")
	}
	if got := CompactionDeclineReason(other); got != "disk went away" {
		t.Fatalf("reason = %q, want the error itself", got)
	}
	if got := CompactionDeclineReason(nil); got != "" {
		t.Fatalf("nil reason = %q, want empty", got)
	}
}
