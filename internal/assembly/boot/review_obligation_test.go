package boot

import (
	"testing"

	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/safety/evidence"
)

// The built-in reviewers declare their own contract. Read from the store the
// way a session reads it, so a definition that lost its declaration fails here
// rather than silently stopping a verdict from being required.
func TestBuiltInReviewersDeclareTheirVerdict(t *testing.T) {
	store := skill.New(skill.Options{})
	for name, want := range map[string]string{
		"review":          skill.ReviewReportReview,
		"security-review": skill.ReviewReportSecurity,
		"explore":         "",
		"research":        "",
	} {
		sk, ok := store.Read(name)
		if !ok {
			t.Fatalf("built-in %q is missing", name)
		}
		if got := sk.Delivery.ReviewReport; got != want {
			t.Errorf("built-in %q declares %q, want %q", name, got, want)
		}
		if got := delegation.ProfileFromSkill(sk).Delivery.ReviewReport; got != evidence.ReviewKind(want) {
			t.Errorf("built-in %q projects %q, want %q", name, got, want)
		}
		// What it may prove is declared beside what it owes, and neither is
		// read off the other: a reviewer authorized for its own obligation and
		// for nothing else is the whole point of the pair.
		authority := delegation.ProfileFromSkill(sk).Authority.Review
		for _, kind := range evidence.ReviewKinds() {
			if got, expect := authority.Proves(kind), want != "" && evidence.ReviewKind(want) == kind; got != expect {
				t.Errorf("built-in %q proves %q = %v, want %v", name, kind, got, expect)
			}
		}
	}
}
