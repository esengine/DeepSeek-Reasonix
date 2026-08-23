package agent

import "testing"

func TestReadinessContinuationClassPriority(t *testing.T) {
	cases := []struct {
		name  string
		check finalReadinessCheck
		want  ReadinessContinuationClass
	}{
		{name: "empty", check: finalReadinessCheck{}, want: ReadinessContinuationNone},
		{name: "generic", check: finalReadinessCheck{reason: "verify", continuationGeneric: true}, want: ReadinessContinuationGeneric},
		{name: "high confidence", check: finalReadinessCheck{reason: "run exact check", continuationGeneric: true, continuationHighConfidence: true}, want: ReadinessContinuationHighConfidence},
		{name: "task progress mutation", check: finalReadinessCheck{reason: "mutate", continuationTaskProgress: true, missingMutation: 1}, want: ReadinessContinuationTaskProgress},
		{name: "task progress deferred action", check: finalReadinessCheck{reason: "continue task", continuationTaskProgress: true, missingTaskProgress: 1}, want: ReadinessContinuationTaskProgress},
		{name: "unsafe beats high confidence", check: finalReadinessCheck{reason: "act", continuationHighConfidence: true, continuationUnsafe: true}, want: ReadinessContinuationNone},
		{name: "action is unsafe", check: finalReadinessCheck{reason: "act", continuationHighConfidence: true, missingActionEvidence: 1}, want: ReadinessContinuationNone},
		{name: "mutation is unsafe", check: finalReadinessCheck{reason: "mutate", continuationHighConfidence: true, missingMutation: 1}, want: ReadinessContinuationNone},
		{name: "capability is unsafe", check: finalReadinessCheck{reason: "capability", continuationHighConfidence: true, missingCapabilities: 1}, want: ReadinessContinuationNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check.continuationClass(); got != tc.want {
				t.Fatalf("continuationClass() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFinalReadinessErrorContinuationDefaultsToNone(t *testing.T) {
	err := &FinalReadinessError{Attempts: 1, Reason: "legacy caller"}
	if err.ContinuationClass != ReadinessContinuationNone {
		t.Fatalf("ContinuationClass = %q, want conservative zero value", err.ContinuationClass)
	}
}
