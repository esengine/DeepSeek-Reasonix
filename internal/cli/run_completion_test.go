package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/agent"
)

func TestClassifyRunCompletionNamesTheFailureClass(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "success"},
		{&agent.FinalReadinessError{Attempts: 3, Reason: "incomplete todos"}, "final_readiness"},
		{&agent.RecoveryPauseError{Message: "paused"}, "recovery_paused"},
		{fmt.Errorf("run: %w", context.DeadlineExceeded), "timeout"},
		{context.Canceled, "cancelled"},
		{errors.New("provider 500"), "error_during_execution"},
	} {
		if got := classifyRunCompletion(tc.err).class; got != tc.want {
			t.Errorf("class for %v = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestFailureClassDoesNotChangeExitBehaviour(t *testing.T) {
	readiness := classifyRunCompletion(&agent.FinalReadinessError{Attempts: 3})
	if !readiness.isError || readiness.exitCode != 1 || readiness.outcome != "" {
		t.Fatalf("final-readiness completion = %+v, want the unchanged error exit", readiness)
	}
	if readiness.subtype != "error_during_execution" {
		t.Fatalf("subtype = %q, want the unchanged wire subtype", readiness.subtype)
	}
}
