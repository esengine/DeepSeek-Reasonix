package evidence

import "testing"

func TestVerificationOutcomeFromPipeStatus(t *testing.T) {
	const masked = "go test ./... 2>&1 | tail -5"

	if got := VerificationOutcomeFromPipeStatus(masked, []int{0, 0}); got != VerificationPassed {
		t.Fatalf("passing suite = %q, want %q", got, VerificationPassed)
	}
	// The shape the whole probe exists for: the suite failed, `tail` succeeded,
	// and the shell reported zero.
	if got := VerificationOutcomeFromPipeStatus(masked, []int{1, 0}); got != VerificationFailed {
		t.Fatalf("failing suite behind a pipe = %q, want %q", got, VerificationFailed)
	}
	if got := VerificationExitConclusive(masked); got {
		t.Fatal("the exit status alone must stay inconclusive for this shape")
	}
}

func TestVerificationOutcomeFromPipeStatusStaysSilentWhenUnsure(t *testing.T) {
	cases := []struct {
		name    string
		command string
		status  []int
	}{
		{"no report", "go test ./... | tail -5", nil},
		{"status count mismatch", "go test ./... | tail -5", []int{0}},
		{"not a single pipeline", "go vet ./... && go test ./...", []int{0, 0}},
		{"no verification stage", "cat log | tail -5", []int{0, 0}},
	}
	for _, tc := range cases {
		if got := VerificationOutcomeFromPipeStatus(tc.command, tc.status); got != "" {
			t.Errorf("%s: verdict = %q, want none", tc.name, got)
		}
	}
}

// The shape that let a red suite read as verified: two checks in one turn, one
// failing, and the shell's exit status belonging to the one that passed.
func TestFailedVerificationIsNotAnsweredByAnotherPassingOne(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"quota.go"}})
	l.Record(Receipt{ToolName: "bash", Success: false, Command: "go test ./...", Verification: VerificationFailed})
	l.Record(Receipt{ToolName: "bash", Success: true, Command: "go vet ./...", Verification: VerificationPassed})

	writer, ok := l.LatestProvenMutationIndex()
	if !ok {
		t.Fatal("expected a proven write")
	}
	if !l.HasSuccessfulVerificationCommandAfter(writer) {
		t.Fatal("go vet did pass — the older gate saw only this")
	}
	if !l.HasFailedVerificationAfter(writer) {
		t.Fatal("the failing suite must still count against the change")
	}
}

// Re-running the same check until it passes is ordinary work, not a standing
// failure: outcomes fold per check and only the latest one counts.
func TestReRunClearsAnEarlierFailure(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"quota.go"}})
	l.Record(Receipt{ToolName: "bash", Success: false, Command: "go test ./...", Verification: VerificationFailed})
	l.Record(Receipt{ToolName: "bash", Success: true, Command: "go test ./...", Verification: VerificationPassed})

	writer, _ := l.LatestProvenMutationIndex()
	if l.HasFailedVerificationAfter(writer) {
		t.Fatal("the re-run passed, so nothing stands failed")
	}
}

// A sign-off citing a check that passed is a cited verification even when the
// review it also owes is missing, so the gate can name the one real gap.
func TestCitedVerificationSeparatesFromReview(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"slug.js"}})
	writer, _ := l.LatestProvenMutationIndex()
	l.Record(Receipt{ToolName: "bash", Success: true, Command: "npm test", Verification: VerificationPassed})
	l.Record(Receipt{ToolName: "complete_step", Success: true, Args: []byte(`{"evidence":[{"kind":"verification","command":"npm test"}]}`)})

	if !l.HasCitedVerificationAfter(writer) {
		t.Fatal("the sign-off cited a check that passed")
	}
	if l.HasSuccessfulDeliverySignoffAfter(writer) {
		t.Fatal("without a review of the change, the sign-off is not complete")
	}

	l.Record(Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"slug.js"}})
	l.Record(Receipt{ToolName: "complete_step", Success: true, Args: []byte(`{"evidence":[{"kind":"verification","command":"npm test"}]}`)})
	if !l.HasSuccessfulDeliverySignoffAfter(writer) {
		t.Fatal("reading the changed file completes the sign-off")
	}
}

// The baseline predicate belongs to a caller that decides by asking the ledger
// more questions. Running it under the ledger's own lock deadlocks, and a
// deadlock here hangs the turn rather than failing it.
func TestBaselinePredicateMayQueryTheLedger(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: MutationProven, Paths: []string{"scratch.go"}, Created: []string{"scratch.go"},
	})
	asked := 0
	probe := func(r Receipt) bool {
		asked++
		return l.CreatedInTurn("scratch.go")
	}
	if _, ok := l.LatestProvenMutationIndexFunc(probe); !ok {
		t.Fatal("expected the write to be found")
	}
	if _, ok := l.LatestSuccessfulWriterIndexFunc(probe); !ok {
		t.Fatal("expected the write to be found")
	}
	if asked != 2 {
		t.Fatalf("predicate ran %d times, want once per query", asked)
	}
}
