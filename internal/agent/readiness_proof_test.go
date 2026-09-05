package agent

import (
	"reflect"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
)

func reviewReceipt(kind evidence.ReviewKind, execID string, grants ...evidence.ReviewKind) evidence.Receipt {
	r := evidence.Receipt{
		ToolName: "review_report", Success: true, ReportKind: kind, SourceExecutionID: execID,
	}
	if len(grants) > 0 {
		r.ReviewAuthority = &evidence.ReviewAuthority{Satisfies: grants}
	}
	return r
}

// Why a candidate cannot prove an obligation is not the same fact as which
// obligations are unmet, and folding the two loses the only part a person can
// act on: a report that was filed and could not be attributed reads exactly
// like no report at all once both are "missing review".
func TestReadinessProofsAreOrthogonalToMissing(t *testing.T) {
	write := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"internal/x/a.go"}}
	cases := []struct {
		name  string
		extra evidence.Receipt
		want  []evidence.ReviewProof
	}{
		{
			"a report no run was granted the authority to file",
			reviewReceipt(evidence.ReviewKindReview, "exec-1", evidence.ReviewKindSecurity),
			[]evidence.ReviewProof{{Kind: evidence.ReviewKindReview, Gap: evidence.ReviewProofUngranted, SourceExecutionID: "exec-1"}},
		},
		{
			"a report the host cannot join back to any run",
			reviewReceipt(evidence.ReviewKindReview, "", evidence.ReviewKindReview),
			[]evidence.ReviewProof{{Kind: evidence.ReviewKindReview, Gap: evidence.ReviewProofUnattributed}},
		},
		{
			// The third state, and the one that must not read like the other two:
			// nothing of that kind was ever offered, so there is no entry at all.
			"security evidence leaves the review obligation without a candidate",
			reviewReceipt(evidence.ReviewKindSecurity, "exec-2", evidence.ReviewKindSecurity),
			nil,
		},
		{
			"a granted report leaves no gap",
			reviewReceipt(evidence.ReviewKindReview, "exec-3", evidence.ReviewKindReview),
			nil,
		},
	}

	var missing [][]string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := readinessLedger(write, tc.extra)
			a := &Agent{task: taskRuntime{ledger: l}, deliveryProfile: true}
			got := a.ReadinessResult()
			if !reflect.DeepEqual(got.Proofs, tc.want) {
				t.Fatalf("Proofs = %+v, want %+v", got.Proofs, tc.want)
			}
			missing = append(missing, got.Missing)
		})
	}
	// The point of the dimension: every arm above reports the same unmet
	// obligations, and the proofs are the only thing telling them apart.
	for i := 1; i < len(missing); i++ {
		if !reflect.DeepEqual(missing[0], missing[i]) {
			t.Fatalf("arm %d changed Missing to %v; the fixture no longer isolates the proof dimension", i, missing[i])
		}
	}
}

// A capability grant closes what it was granted for and nothing beside it.
func TestSecurityAuthorityCannotCloseReview(t *testing.T) {
	l := readinessLedger(
		evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}},
		reviewReceipt(evidence.ReviewKindReview, "exec-1", evidence.ReviewKindSecurity),
	)
	filed := reviewReceipt(evidence.ReviewKindReview, "exec-1", evidence.ReviewKindSecurity)
	if evidence.ReceiptProves(filed, evidence.ReviewKindReview) {
		t.Fatal("a security grant proved a review obligation")
	}
	if !evidence.ReceiptProves(reviewReceipt(evidence.ReviewKindSecurity, "exec-1", evidence.ReviewKindSecurity), evidence.ReviewKindSecurity) {
		t.Fatal("a security grant failed to prove the obligation it was granted for")
	}
	if got := l.ReviewProofFor(evidence.ReviewKindReview); got.Gap != evidence.ReviewProofUngranted {
		t.Fatalf("review proof gap = %v, want ungranted", got.Gap)
	}
}

// The completion gate and the current-readiness projection read the same
// judgement. They share one evaluator today; this is what fails if either side
// ever grows arithmetic of its own — the failure that matters is not a wording
// drift but two answers to "may this turn end", one of which a person is shown.
func TestCompletionGateAndCurrentReadinessAgree(t *testing.T) {
	check := instruction.VerifyCheck{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}
	write := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	verify := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."}
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	signoff := evidence.Receipt{ToolName: "complete_step", Success: true, Step: "edit",
		Args: []byte(`{"evidence":[{"kind":"verification","command":"go test ./..."}]}`)}

	for _, tc := range []struct {
		name     string
		checks   []instruction.VerifyCheck
		receipts []evidence.Receipt
		delivery bool
	}{
		{"nothing done", nil, nil, false},
		{"wrote and stopped", nil, []evidence.Receipt{write}, true},
		{"wrote, verified, signed off", nil, []evidence.Receipt{write, verify, signoff}, true},
		{"project check owed", []instruction.VerifyCheck{check}, []evidence.Receipt{verify, write}, false},
		{"list left open", nil, []evidence.Receipt{write, todo}, false},
		{"report the host cannot attribute", nil,
			[]evidence.Receipt{write, reviewReceipt(evidence.ReviewKindReview, "", evidence.ReviewKindReview)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{task: taskRuntime{ledger: readinessLedger(tc.receipts...)}, projectChecks: tc.checks, deliveryProfile: tc.delivery}
			gate := a.finalReadinessCheckFor()
			shown := a.ReadinessResult()

			if refused := gate.reason != ""; refused == shown.Ready {
				t.Fatalf("gate refuses=%v while the projection says ready=%v", refused, shown.Ready)
			}
			want := gate.missingIDs()
			if shown.Ready {
				want = nil
			}
			if !reflect.DeepEqual(nilIfEmpty(shown.Missing), nilIfEmpty(want)) {
				t.Fatalf("projection missing %v, gate missing %v", shown.Missing, want)
			}
			if !reflect.DeepEqual(shown.Proofs, a.reviewProofs()) {
				t.Fatalf("projection proofs %+v, ledger proofs %+v", shown.Proofs, a.reviewProofs())
			}
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
