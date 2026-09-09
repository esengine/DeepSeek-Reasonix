package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestFinalReadinessErrorPreservesDiagnosticText(t *testing.T) {
	err := (&FinalReadinessError{Attempts: 3, Reason: "missing verification"}).Error()
	want := "final-answer readiness failed 3 times: missing verification"
	if err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}
}

func TestPauseClassNamesEachGuard(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&maxStepsPause{steps: 40, key: "max_steps"}, "max_steps"},
		{&FinalReadinessError{Attempts: 3}, "final_readiness"},
		{&RecoveryPauseError{Message: "paused"}, "recovery_paused"},
		{&IncompleteReadError{Reason: "continuation ignored"}, "incomplete_read"},
		{fmt.Errorf("wrapped: %w", &maxStepsPause{steps: 5}), "max_steps"},
		{errors.New("provider 500"), ""},
		{nil, ""},
	} {
		if got := PauseClass(tc.err); got != tc.want {
			t.Errorf("PauseClass(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestIncompleteReadIsAHostOwnedRecoverablePause(t *testing.T) {
	info, ok := InspectRunPause(&IncompleteReadError{Reason: "continuation ignored"})
	if !ok || info.Kind != "incomplete_read" || !info.HostOwned || info.Reason != "continuation ignored" {
		t.Fatalf("InspectRunPause = %+v, %v", info, ok)
	}
}
