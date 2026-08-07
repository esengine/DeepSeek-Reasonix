package autoresearch

import (
	"testing"
	"time"
)

func TestValidateFindingAcceptsProtocolKinds(t *testing.T) {
	base := Finding{
		ID:        "f1",
		Summary:   "s",
		CreatedAt: time.Now().UTC(),
	}
	// Protocol evidence blocks use kind:"file" and kind:"verification".
	for _, kind := range []string{
		FindingKindCommand, FindingKindFile, FindingKindTest,
		FindingKindBenchmark, FindingKindManual, FindingKindReview,
		FindingKindVerification,
	} {
		f := base
		f.Kind = kind
		if err := validateFinding(f); err != nil {
			t.Errorf("kind %q should be accepted: %v", kind, err)
		}
	}
	// Unknown kinds stay rejected (fail-closed).
	bad := base
	bad.Kind = "objective"
	if err := validateFinding(bad); err == nil {
		t.Error("unknown kind must be rejected")
	}
	// Missing summary rejected.
	noSummary := base
	noSummary.Kind = FindingKindVerification
	noSummary.Summary = ""
	if err := validateFinding(noSummary); err == nil {
		t.Error("empty summary must be rejected")
	}
}
