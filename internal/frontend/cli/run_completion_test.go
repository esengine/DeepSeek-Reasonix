package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/runtime/agent"
)

func TestClassifyRunCompletionNamesTheFailureClass(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "success"},
		{&agent.FinalReadinessError{Attempts: 3, Reason: "incomplete todos"}, "final_readiness"},
		{fmt.Errorf("run: %w", context.DeadlineExceeded), "timeout"},
		{context.Canceled, "cancelled"},
		{errors.New("provider 500"), "error_during_execution"},
	} {
		if got := classifyRunCompletion(tc.err).class; got != tc.want {
			t.Errorf("class for %v = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// An unmet final-readiness judgement is a verdict on a finished run: it keeps
// its benchmark class but exits 0 unless --fail-on-unverified asks for 3.
func TestFinalReadinessIsAVerdictNotAFailure(t *testing.T) {
	readiness := classifyRunCompletion(&agent.FinalReadinessError{Attempts: 3})
	if readiness.isError || readiness.exitCode != 0 || !readiness.unverified || readiness.subtype != "success" {
		t.Fatalf("final-readiness completion = %+v, want a successful, unverified run", readiness)
	}
	if got := readiness.withFailOnUnverified(true).exitCode; got != runExitUnverified {
		t.Fatalf("--fail-on-unverified exit = %d, want %d", got, runExitUnverified)
	}
	if got := classifyRunCompletion(errors.New("provider 500")).withFailOnUnverified(true).exitCode; got != 1 {
		t.Fatalf("--fail-on-unverified changed a run error's exit to %d", got)
	}
}
