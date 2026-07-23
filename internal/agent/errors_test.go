package agent

import "testing"

func TestFinalReadinessErrorPreservesDiagnosticText(t *testing.T) {
	err := (&FinalReadinessError{Attempts: 3, Reason: "missing verification"}).Error()
	want := "final-answer readiness failed 3 times: missing verification"
	if err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}
}
