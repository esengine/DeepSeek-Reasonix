package completion

import (
	"slices"
	"testing"

	"reasonix/internal/safety/evidence"
)

// classified is a bash receipt carrying the host's verification verdict, which
// is what a real run records; ran() leaves it empty on purpose.
func classified(command, verdict string, ok bool) evidence.Receipt {
	r := ran(command, ok)
	r.Verification = verdict
	return r
}

// A suite piped into another command exits with that command's status, so a
// zero exit is not proof the suite passed.
func TestPipedVerificationIsNotProof(t *testing.T) {
	ledger := ledgerOf(
		wrote("parse.go"),
		classified("go test ./... 2>&1 | head -100", evidence.VerificationInconclusive, true),
	)
	rep := Build(nil, ledger, nil)
	if len(rep.Verifications) != 1 || !rep.Verifications[0].Inconclusive {
		t.Fatalf("verifications = %+v, want one inconclusive entry", rep.Verifications)
	}
	if rep.Verifications[0].Passed {
		t.Fatalf("verifications = %+v, want no claim that the suite passed", rep.Verifications)
	}
	if !slices.Contains(gapKinds(rep), "inconclusive_verification") {
		t.Fatalf("gap kinds = %v, want the unreadable verdict named", gapKinds(rep))
	}
}

// A trailing command that fails owns the exit status, so the suite that ran
// before it must not be reported as the failure.
func TestTrailingFailureIsNotTheVerificationsFailure(t *testing.T) {
	ledger := ledgerOf(
		wrote("parse.go"),
		classified("go test ./... && git status --short", evidence.VerificationInconclusive, false),
	)
	rep := Build(nil, ledger, nil)
	if slices.Contains(gapKinds(rep), "failed_verification") {
		t.Fatalf("gap kinds = %v, want no failure attributed to the suite", gapKinds(rep))
	}
}

// Once something fresh and readable proves the tree, an unreadable run of some
// other command is pedantry, not a gap.
func TestInconclusiveIsQuietOnceSomethingFreshPassed(t *testing.T) {
	ledger := ledgerOf(
		wrote("parse.go"),
		read("parse.go"),
		classified("go vet ./... && go test ./...", evidence.VerificationPassed, true),
		classified("go test -v ./... | tail -20 && git status", evidence.VerificationInconclusive, false),
	)
	rep := Build(nil, ledger, nil)
	if kinds := gapKinds(rep); len(kinds) != 0 {
		t.Fatalf("gap kinds = %v, want none: a readable run already proved the tree", kinds)
	}
}

// The same check inside a different wrapper is the same check: its fresh
// outcome must supersede the earlier one rather than sit beside it.
func TestSameCheckInADifferentWrapperIsOneItem(t *testing.T) {
	ledger := ledgerOf(
		classified("cd /w && ls -la && go test ./... 2>&1 | head -100", evidence.VerificationInconclusive, true),
		wrote("parse.go"),
		classified("cd /w && go test ./...", evidence.VerificationPassed, true),
	)
	rep := Build(nil, ledger, nil)
	if len(rep.Verifications) != 1 {
		t.Fatalf("verifications = %+v, want one item for one check", rep.Verifications)
	}
	v := rep.Verifications[0]
	if !v.Passed || v.Stale || v.Inconclusive {
		t.Fatalf("verification = %+v, want the fresh passing run to win", v)
	}
}

// Receipts that no host classified (declared commands, replayed sessions) keep
// reading their own outcome, so the switch to host verdicts adds no gap.
func TestUnclassifiedReceiptKeepsItsOwnOutcome(t *testing.T) {
	ledger := ledgerOf(wrote("parse.go"), read("parse.go"), ran("go test ./...", true))
	rep := Build(nil, ledger, nil)
	if len(rep.Verifications) != 1 || !rep.Verifications[0].Passed || rep.Verifications[0].Inconclusive {
		t.Fatalf("verifications = %+v, want the plain successful run to count", rep.Verifications)
	}
	if kinds := gapKinds(rep); len(kinds) != 0 {
		t.Fatalf("gap kinds = %v, want none", kinds)
	}
}
