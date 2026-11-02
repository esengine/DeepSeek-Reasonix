package evidence

import (
	"testing"
)

// TestVerificationCommandSummaryAcceptedPipeline ensures the escape hatch
// advertised by the summary (a read-only extraction pipeline ending in a
// recognized verifier) really is accepted by the classifier.
func TestVerificationCommandSummaryAcceptedPipeline(t *testing.T) {
	for _, cmd := range []string{
		"tail -n +1 out.json | node --check -",
		"cat file.js | node --check -",
	} {
		if !IsDeliveryVerificationCommand(cmd) {
			t.Errorf("advertised read-only pipeline %q rejected by classifier", cmd)
		}
	}
}
