package evidence

import (
	"slices"
	"strings"
	"testing"
)

func owed(l *Ledger, checks ...string) []ObligationKind {
	var kinds []ObligationKind
	for _, o := range l.Obligations(CaptureCheckContract(checks, checks)) {
		kinds = append(kinds, o.Kind)
	}
	slices.Sort(kinds)
	return kinds
}

func ledgerOf(receipts ...Receipt) *Ledger {
	l := NewLedger()
	for _, r := range receipts {
		l.Record(r)
	}
	return l
}

var (
	opaqueWrite = Receipt{ToolName: "bash", Success: true, Write: true, Mutation: true,
		MutationEvidence: MutationUnknown, Command: `python3 -c 'open("a","w")'`}
	observedWrite = Receipt{ToolName: "bash", Success: true, Write: true, Mutation: true,
		MutationEvidence: MutationProven, Paths: []string{"a.go"}, Command: `python3 -c 'open("a","w")'`}
	passingCheck = Receipt{ToolName: "bash", Success: true, Command: "go test ./..."}
)

func TestObligationsAreDerivedFromTheLedger(t *testing.T) {
	cases := []struct {
		name     string
		ledger   *Ledger
		checks   []string
		wantKind []ObligationKind
	}{
		{"an empty ledger owes nothing", ledgerOf(), nil, nil},
		{
			name:     "a change the host could not scope owes both",
			ledger:   ledgerOf(opaqueWrite),
			wantKind: []ObligationKind{ObligationStaleVerification, ObligationUnprovenMutation},
		},
		{
			name:     "a change it could scope owes only the check",
			ledger:   ledgerOf(observedWrite),
			wantKind: []ObligationKind{ObligationStaleVerification},
		},
		{
			name:   "a check after the change settles it",
			ledger: ledgerOf(observedWrite, passingCheck),
		},
		{
			name:     "a check declared when the task began is owed as baseline",
			ledger:   ledgerOf(observedWrite, passingCheck),
			checks:   []string{"go vet ./..."},
			wantKind: []ObligationKind{ObligationBaselineCheck},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := owed(tc.ledger, tc.checks...); !slices.Equal(got, tc.wantKind) {
				t.Fatalf("obligations = %v, want %v", got, tc.wantKind)
			}
		})
	}
}

// One action can settle a debt and create another in the same breath:
// establishing what a change touched answers for the effect and leaves the
// checks before it answering for a workspace that no longer exists. Reporting
// only creations would hide the transition.
func TestDiffObligationsReportsBothHalvesOfATransition(t *testing.T) {
	before := ledgerOf(opaqueWrite).Obligations(CheckContract{})
	after := ledgerOf(observedWrite).Obligations(CheckContract{})

	delta := DiffObligations(before, after)
	if len(delta.Discharged) != 1 || delta.Discharged[0].Kind != ObligationUnprovenMutation {
		t.Fatalf("discharged = %+v, want the unproven mutation", delta.Discharged)
	}
	if len(delta.Added) != 0 {
		t.Fatalf("added = %+v, want none — the stale check was already standing", delta.Added)
	}
	if delta.Empty() {
		t.Fatal("a delta that settled a debt reports itself empty")
	}
}

// A debt keeps one identity while it stands, so the same one seen twice is not
// reported as a second debt.
func TestDiffObligationsKeepsAStandingDebtQuiet(t *testing.T) {
	owed := ledgerOf(opaqueWrite).Obligations(CheckContract{})
	if delta := DiffObligations(owed, owed); !delta.Empty() {
		t.Fatalf("delta = %+v, want nothing to report", delta)
	}
}

func TestDiffObligationsReportsANewDebt(t *testing.T) {
	delta := DiffObligations(ledgerOf().Obligations(CheckContract{}), ledgerOf(opaqueWrite).Obligations(CheckContract{}))
	var kinds []ObligationKind
	for _, o := range delta.Added {
		kinds = append(kinds, o.Kind)
	}
	slices.Sort(kinds)
	want := []ObligationKind{ObligationStaleVerification, ObligationUnprovenMutation}
	if !slices.Equal(kinds, want) {
		t.Fatalf("added = %v, want %v", kinds, want)
	}
	if delta.Added[0].ID == "" || delta.Added[0].Discharge == "" {
		t.Fatalf("obligation = %+v, want an identity and what settles it", delta.Added[0])
	}
}

// The exploit this exists for: rewrite the declaration so the check that would
// have failed is no longer required, run the replacement, and claim the exam is
// over. The requirement the task began under is the host's, not the project's
// to retract.
func TestRemovingADeclaredCheckDoesNotEraseItsBaselineObligation(t *testing.T) {
	contract := CaptureCheckContract([]string{"go test ./..."}, []string{"true"})
	l := ledgerOf(observedWrite, Receipt{ToolName: "bash", Success: true, Command: "true"})

	var baseline *Obligation
	for _, o := range l.Obligations(contract) {
		if o.Kind == ObligationBaselineCheck {
			baseline = &o
		}
	}
	if baseline == nil {
		t.Fatal("the rewritten declaration cancelled the requirement the task began with")
	}
	if !strings.Contains(baseline.Cause, "rewritten") {
		t.Fatalf("cause = %q, want the rewrite reported as the observation it is", baseline.Cause)
	}
	if !strings.Contains(baseline.Discharge, "go test ./...") {
		t.Fatalf("discharge = %q, want the original check named", baseline.Discharge)
	}
}

// Running the baseline check itself is what settles it — the same discharge as
// any other criterion, reached through its identity rather than its spelling.
func TestEvidenceForTheBaselineCheckSettlesTheBaselineObligation(t *testing.T) {
	contract := CaptureCheckContract([]string{"go test ./..."}, []string{"true"})
	ran := Receipt{ToolName: "bash", Success: true, Command: "cd repo && go test ./..."}
	for _, o := range ledgerOf(observedWrite, ran).Obligations(contract) {
		if o.Kind == ObligationBaselineCheck {
			t.Fatalf("obligation %+v still owed after the baseline check ran", o)
		}
	}
}

// A project may require more of itself at any time. What it may not do is have
// that change anything about what it already required.
func TestAddingANewCheckDoesNotRewriteBaselineProvenance(t *testing.T) {
	contract := CaptureCheckContract([]string{"go test ./..."}, []string{"go test ./...", "go vet ./..."})
	var kinds []ObligationKind
	for _, o := range ledgerOf(observedWrite).Obligations(contract) {
		if o.Kind == ObligationBaselineCheck || o.Kind == ObligationMissingProjectCheck {
			kinds = append(kinds, o.Kind)
		}
	}
	slices.Sort(kinds)
	want := []ObligationKind{ObligationBaselineCheck, ObligationMissingProjectCheck}
	if !slices.Equal(kinds, want) {
		t.Fatalf("kinds = %v, want the baseline kept and the new check owed on its own", kinds)
	}
}

// A declaration nobody touched must not read as a rewrite, or every action
// would report criteria churn that never happened.
func TestUnchangedDeclarationDoesNotCreateCriteriaNoise(t *testing.T) {
	contract := CaptureCheckContract([]string{"go test ./..."}, []string{"go test ./..."})
	owed := ledgerOf(observedWrite).Obligations(contract)
	if delta := DiffObligations(owed, ledgerOf(observedWrite).Obligations(contract)); !delta.Empty() {
		t.Fatalf("delta = %+v, want nothing to report", delta)
	}
	for _, o := range owed {
		if o.Kind == ObligationBaselineCheck && strings.Contains(o.Cause, "rewritten") {
			t.Fatalf("cause = %q, want no rewrite claimed for an untouched declaration", o.Cause)
		}
	}
}

// Identity is the host's reading of the invocation, so the same check written
// differently is the same check — otherwise ordinary formatting reads as the
// project quietly replacing its own criteria.
func TestReorderingOrCanonicalEquivalentDeclarationsKeepIdentity(t *testing.T) {
	plain := CaptureCheckContract([]string{"go test ./...", "go vet ./..."}, nil)
	respelt := CaptureCheckContract([]string{"go   vet   ./... ", "  go test ./...  "}, nil)
	if !slices.Equal(plain.Baseline(), []string{"go test ./...", "go vet ./..."}) {
		t.Fatalf("baseline = %q, want the canonical invocations", plain.Baseline())
	}
	got, want := respelt.Baseline(), []string{"go vet ./...", "go test ./..."}
	if !slices.Equal(got, want) {
		t.Fatalf("baseline = %q, want the same identities from a different spelling", got)
	}
}

// A page this workspace serves, acted on after the write, is the check a
// browser task has: the host asked for a shell command afterwards and sent the
// turn back for one, three times in a real session.
func TestAPassingBrowserCheckSettlesTheWriteItFollowed(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, MutationEvidence: MutationProven, Paths: []string{"app.js"}})
	owed := func() bool {
		for _, o := range l.Obligations(CheckContract{}) {
			if o.Kind == ObligationStaleVerification {
				return true
			}
		}
		return false
	}
	if !owed() {
		t.Fatal("a write with no check after it owes a verification")
	}
	l.Record(Receipt{ToolName: "browser_act", Success: true, Verification: VerificationPassed})
	if owed() {
		t.Fatal("a passing browser check left the write owing a verification")
	}
}
