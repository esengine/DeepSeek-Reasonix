package completion

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

func ledgerOf(receipts ...evidence.Receipt) *evidence.Ledger {
	l := evidence.NewLedger()
	for _, r := range receipts {
		l.Record(r)
	}
	return l
}

func wrote(path string) evidence.Receipt {
	return evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{path}}
}

func read(path string) evidence.Receipt {
	return evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{path}, OutputBytes: 64}
}

func ran(command string, ok bool) evidence.Receipt {
	return evidence.Receipt{ToolName: "bash", Success: ok, Command: command, OutputBytes: 64}
}

func gapKinds(rep Report) []string { return rep.GapKinds() }

func TestBuildWithNothingToJudgeStaysUnknown(t *testing.T) {
	rep := Build(nil, evidence.NewLedger())
	if rep.Verdict != VerdictUnknown {
		t.Fatalf("verdict = %v, want unknown for a turn with no contract and no receipts", rep.Verdict)
	}
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none", rep.Gaps)
	}
}

func TestBuildIsDoneWhenChangeIsVerifiedAndReviewed(t *testing.T) {
	c := taskcontract.New("fix the parser")
	c.AddRequirement("r1", "parser accepts empty input", true)
	c.AddCheck("go test ./...")
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", true))
	for _, r := range ledger.Receipts() {
		c.Observe(r)
	}
	c.Resolve("r1", taskcontract.Satisfied, taskcontract.EvidenceRef{Kind: taskcontract.EvidenceVerification, MutationEpoch: c.Epoch(), Success: true})

	rep := Build(c, ledger)
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v (%s), want done; gaps %+v", rep.Verdict, rep.Summary(), rep.Gaps)
	}
	if len(rep.Changes) != 1 || !rep.Changes[0].Reviewed {
		t.Fatalf("changes = %+v, want parser.go reviewed", rep.Changes)
	}
	if len(rep.Verifications) != 1 || !rep.Verifications[0].Passed || rep.Verifications[0].Stale {
		t.Fatalf("verifications = %+v, want one fresh pass", rep.Verifications)
	}
	if rep.Criteria[0].Proofs != 1 {
		t.Fatalf("criterion proofs = %d, want 1", rep.Criteria[0].Proofs)
	}
}

func TestBuildReportsUnreviewedChangeAsPartial(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), ran("go test ./...", true))

	rep := Build(nil, ledger)
	if rep.Verdict != VerdictPartial {
		t.Fatalf("verdict = %v, want partial: a verified but never-inspected change is not a clean done", rep.Verdict)
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"unreviewed_change"}) {
		t.Fatalf("gap kinds = %v, want [unreviewed_change]", got)
	}
	if rep.Gaps[0].Detail != "parser.go" {
		t.Fatalf("gap detail = %q, want the unreviewed path", rep.Gaps[0].Detail)
	}
}

func TestBuildReportsMutationWithNoVerificationAtAll(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"))

	rep := Build(nil, ledger)
	if got := gapKinds(rep); !slices.Equal(got, []string{"unverified_change"}) {
		t.Fatalf("gap kinds = %v, want [unverified_change]", got)
	}
	if rep.Verdict != VerdictPartial {
		t.Fatalf("verdict = %v, want partial", rep.Verdict)
	}
}

func TestBuildKeepsFailedVerificationVisible(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", false))

	rep := Build(nil, ledger)
	if len(rep.Verifications) != 1 || rep.Verifications[0].Passed {
		t.Fatalf("verifications = %+v, want the failing run recorded", rep.Verifications)
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"failed_verification", "unverified_change"}) {
		t.Fatalf("gap kinds = %v, want the failure and the resulting unverified change", got)
	}
}

func TestBuildStalesVerificationThatPredatesTheLatestChange(t *testing.T) {
	ledger := ledgerOf(ran("go test ./...", true), wrote("parser.go"), read("parser.go"))

	rep := Build(nil, ledger)
	if !rep.Verifications[0].Stale {
		t.Fatalf("verification = %+v, want stale: it ran before the change", rep.Verifications[0])
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"stale_verification", "unverified_change"}) {
		t.Fatalf("gap kinds = %v", got)
	}
}

func TestBuildLatestRunOfACommandWins(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", false), ran("go test ./...", true))

	rep := Build(nil, ledger)
	if len(rep.Verifications) != 1 || !rep.Verifications[0].Passed {
		t.Fatalf("verifications = %+v, want one entry showing the latest (passing) run", rep.Verifications)
	}
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none once the retry passed", rep.Gaps)
	}
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v, want done", rep.Verdict)
	}
}

func TestBuildIncompleteWhenARequiredCriterionHasNoProof(t *testing.T) {
	c := taskcontract.New("fix the parser")
	c.AddRequirement("r1", "parser accepts empty input", true)
	c.AddRequirement("r2", "nice to have", false)
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", true))
	for _, r := range ledger.Receipts() {
		c.Observe(r)
	}

	rep := Build(c, ledger)
	if rep.Verdict != VerdictIncomplete {
		t.Fatalf("verdict = %v, want incomplete", rep.Verdict)
	}
	if got := gapKinds(rep); !slices.Contains(got, "unproven_criterion") {
		t.Fatalf("gap kinds = %v, want an unproven criterion", got)
	}
	for _, gap := range rep.Gaps {
		if strings.Contains(gap.Detail, "nice to have") {
			t.Fatalf("optional criterion must not become a gap: %+v", gap)
		}
	}
}

func TestBuildDeclaredCheckReplacesTheBlanketGap(t *testing.T) {
	c := taskcontract.New("fix the parser")
	c.AddCheck("go vet ./...")
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"))
	for _, r := range ledger.Receipts() {
		c.Observe(r)
	}

	rep := Build(c, ledger)
	if got := gapKinds(rep); !slices.Equal(got, []string{"missing_check"}) {
		t.Fatalf("gap kinds = %v, want only the specific missing check", got)
	}
	if rep.Gaps[0].Detail != "go vet ./..." {
		t.Fatalf("gap detail = %q, want the declared command", rep.Gaps[0].Detail)
	}
}

func TestBuildReviewIsScopedPerPath(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), wrote("lexer.go"), read("parser.go"), ran("go test ./...", true))

	rep := Build(nil, ledger)
	if len(rep.Changes) != 2 {
		t.Fatalf("changes = %+v, want both paths", rep.Changes)
	}
	if !rep.Changes[0].Reviewed || rep.Changes[1].Reviewed {
		t.Fatalf("changes = %+v: reading parser.go must not vouch for lexer.go", rep.Changes)
	}
	if rep.Gaps[0].Detail != "lexer.go" {
		t.Fatalf("gap = %+v, want the unreviewed path only", rep.Gaps[0])
	}
}

func TestBuildRewritingAPathAfterReviewReopensIt(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", true), wrote("parser.go"))

	rep := Build(nil, ledger)
	if rep.Changes[0].Reviewed {
		t.Fatalf("change = %+v, want unreviewed: the last write came after the read", rep.Changes[0])
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"stale_verification", "unverified_change", "unreviewed_change"}) {
		t.Fatalf("gap kinds = %v, want the late edit to stale every proof", got)
	}
}

func TestBuildCountsAPathlessMutation(t *testing.T) {
	shell := evidence.Receipt{ToolName: "bash", Success: true, Command: "sed -i '' s/a/b/ parser.go", Mutation: true, OutputBytes: 8}
	rep := Build(nil, ledgerOf(shell))
	if rep.Mutations != 1 || len(rep.Changes) != 0 {
		t.Fatalf("mutations = %d, changes = %+v: a path-less mutation still changed the workspace", rep.Mutations, rep.Changes)
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"unverified_change"}) {
		t.Fatalf("gap kinds = %v, want the mutation flagged even with no path to name", got)
	}
}

func TestSummaryCountsRequiredCriteriaOnly(t *testing.T) {
	c := taskcontract.New("fix the parser")
	c.AddRequirement("r1", "done", true)
	c.AddRequirement("r2", "optional", false)
	c.Resolve("r1", taskcontract.Satisfied)
	rep := Build(c, ledgerOf())
	if got := rep.Summary(); !strings.Contains(got, "criteria 1/1") {
		t.Fatalf("summary = %q, want required-only criteria counts", got)
	}
}

// Listing every earlier form of the same check after a green run is pedantry,
// and pedantry is how a receipt teaches people to skip it.
func TestStaleCommandIsNotAGapOnceSomethingFreshPassed(t *testing.T) {
	rep := Build(nil, ledgerOf(
		ran("python3 -m pytest -q", true),
		wrote("parser.go"),
		read("parser.go"),
		ran("python3 -m unittest discover", true),
	))
	if got := gapKinds(rep); len(got) != 0 {
		t.Fatalf("gap kinds = %v, want none: the fresh run proved the tree", got)
	}
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v, want done", rep.Verdict)
	}
}

// With nothing fresh, the stale command is still the only thing standing
// between the work and an unverified claim, so it must be named.
func TestStaleCommandIsAGapWhenNothingFreshPassed(t *testing.T) {
	rep := Build(nil, ledgerOf(
		ran("go test ./...", true),
		wrote("parser.go"),
		read("parser.go"),
	))
	if got := gapKinds(rep); !slices.Contains(got, "stale_verification") {
		t.Fatalf("gap kinds = %v, want the stale command named", got)
	}
}
