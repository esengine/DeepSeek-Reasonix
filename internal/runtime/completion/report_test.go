package completion

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
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

func gapDetail(rep Report, kind string) string {
	for _, gap := range rep.Gaps {
		if gap.Kind.String() == kind {
			return gap.Detail
		}
	}
	return ""
}

func TestBuildWithNothingToJudgeStaysUnknown(t *testing.T) {
	rep := Build(nil, evidence.NewLedger(), nil)
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

	rep := Build(c, ledger, nil)
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

// Nothing ran and nothing looked: the change is unproven twice over, and the
// path is named so the reader knows which one to go and check.
func TestBuildReportsAnUnprovenChangeAsPartial(t *testing.T) {
	rep := Build(nil, ledgerOf(wrote("parser.go")), nil)

	if rep.Verdict != VerdictPartial {
		t.Fatalf("verdict = %v, want partial: nothing proved this change", rep.Verdict)
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"unverified_change", "unreviewed_change"}) {
		t.Fatalf("gap kinds = %v, want both absences named", got)
	}
	if got := gapDetail(rep, "unreviewed_change"); got != "parser.go" {
		t.Fatalf("gap detail = %q, want the unreviewed path", got)
	}
}

// A fresh passing run is the proof. Asking for a read-back on top of it asks
// for a habit, not for evidence — and a gap that fires on correct work is the
// one that teaches people to stop reading the receipt.
func TestBuildTreatsAFreshPassAsProofOfTheChangeItCovers(t *testing.T) {
	rep := Build(nil, ledgerOf(wrote("parser.go"), ran("go test ./...", true)), nil)

	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v (%s), want done; gaps %+v", rep.Verdict, rep.Summary(), rep.Gaps)
	}
	if len(rep.Changes) != 1 || rep.Changes[0].Reviewed {
		t.Fatalf("changes = %+v: the run proved the change, it did not look at it", rep.Changes)
	}
}

// A pass that predates the newest write proves nothing about it, so the
// read-back is owed again.
func TestBuildStaleProofDoesNotCoverALaterChange(t *testing.T) {
	rep := Build(nil, ledgerOf(wrote("parser.go"), ran("go test ./...", true), wrote("lexer.go")), nil)

	if !slices.Contains(gapKinds(rep), "unreviewed_change") {
		t.Fatalf("gap kinds = %v, want the change made after the run still named", gapKinds(rep))
	}
}

func TestBuildReportsMutationWithNoVerificationAtAll(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"))

	rep := Build(nil, ledger, nil)
	if got := gapKinds(rep); !slices.Equal(got, []string{"unverified_change"}) {
		t.Fatalf("gap kinds = %v, want [unverified_change]", got)
	}
	if rep.Verdict != VerdictPartial {
		t.Fatalf("verdict = %v, want partial", rep.Verdict)
	}
}

func TestBuildKeepsFailedVerificationVisible(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", false))

	rep := Build(nil, ledger, nil)
	if len(rep.Verifications) != 1 || rep.Verifications[0].Passed {
		t.Fatalf("verifications = %+v, want the failing run recorded", rep.Verifications)
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"failed_verification", "unverified_change"}) {
		t.Fatalf("gap kinds = %v, want the failure and the resulting unverified change", got)
	}
}

func TestBuildStalesVerificationThatPredatesTheLatestChange(t *testing.T) {
	ledger := ledgerOf(ran("go test ./...", true), wrote("parser.go"), read("parser.go"))

	rep := Build(nil, ledger, nil)
	if !rep.Verifications[0].Stale {
		t.Fatalf("verification = %+v, want stale: it ran before the change", rep.Verifications[0])
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"stale_verification", "unverified_change"}) {
		t.Fatalf("gap kinds = %v", got)
	}
}

func TestBuildLatestRunOfACommandWins(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", false), ran("go test ./...", true))

	rep := Build(nil, ledger, nil)
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

	rep := Build(c, ledger, nil)
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

	rep := Build(c, ledger, nil)
	if got := gapKinds(rep); !slices.Equal(got, []string{"missing_check"}) {
		t.Fatalf("gap kinds = %v, want only the specific missing check", got)
	}
	if rep.Gaps[0].Detail != "go vet ./..." {
		t.Fatalf("gap detail = %q, want the declared command", rep.Gaps[0].Detail)
	}
}

func TestBuildReviewIsScopedPerPath(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), wrote("lexer.go"), read("parser.go"))

	rep := Build(nil, ledger, nil)
	if len(rep.Changes) != 2 {
		t.Fatalf("changes = %+v, want both paths", rep.Changes)
	}
	if !rep.Changes[0].Reviewed || rep.Changes[1].Reviewed {
		t.Fatalf("changes = %+v: reading parser.go must not vouch for lexer.go", rep.Changes)
	}
	if got := gapDetail(rep, "unreviewed_change"); got != "lexer.go" {
		t.Fatalf("gap detail = %q, want the unreviewed path only: %+v", got, rep.Gaps)
	}
}

func TestBuildRewritingAPathAfterReviewReopensIt(t *testing.T) {
	ledger := ledgerOf(wrote("parser.go"), read("parser.go"), ran("go test ./...", true), wrote("parser.go"))

	rep := Build(nil, ledger, nil)
	if rep.Changes[0].Reviewed {
		t.Fatalf("change = %+v, want unreviewed: the last write came after the read", rep.Changes[0])
	}
	if got := gapKinds(rep); !slices.Equal(got, []string{"stale_verification", "unverified_change", "unreviewed_change"}) {
		t.Fatalf("gap kinds = %v, want the late edit to stale every proof", got)
	}
}

func TestBuildCountsAPathlessMutation(t *testing.T) {
	shell := evidence.Receipt{ToolName: "bash", Success: true, Command: "sed -i '' s/a/b/ parser.go", Mutation: true, OutputBytes: 8}
	rep := Build(nil, ledgerOf(shell), nil)
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
	rep := Build(c, ledgerOf(), nil)
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
	), nil)
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
	), nil)
	if got := gapKinds(rep); !slices.Contains(got, "stale_verification") {
		t.Fatalf("gap kinds = %v, want the stale command named", got)
	}
}

// Reading back a file the turn wrote end to end compares the model's text with
// itself. A later edit to the same file restores the question — there is a
// before again — so the review requirement comes back with it.
func TestAuthoredFilesNeedNoSeparateReview(t *testing.T) {
	ledger := evidence.NewLedger()
	authored := evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{"tally.go"}, Created: []string{"tally.go"},
	}
	ledger.Record(authored)
	changes := changesOf(ledger, ledger.Receipts(), nil)
	if len(changes) != 1 || !changes[0].Reviewed {
		t.Fatalf("changes = %+v, want the authored file to need no review", changes)
	}

	ledger.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven, Paths: []string{"tally.go"},
	})
	changes = changesOf(ledger, ledger.Receipts(), nil)
	if len(changes) != 1 || changes[0].Reviewed {
		t.Fatalf("changes = %+v, want an edit over authored content to need review", changes)
	}
}

// A project that asks for a failing test before the fix produces one every
// time. Once the fix has been proven fresh, reporting that failure back
// reports the bug as an outcome of the work that removed it.
func TestFailureBeforeTheFixIsNotAGapOnceProven(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: false, Command: "go test -run TestNewCase ./...", Verification: evidence.VerificationFailed})
	ledger.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven, Paths: []string{"window.go"},
	})
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"window.go"}})
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./...", Verification: evidence.VerificationPassed})

	rep := Build(nil, ledger, nil)
	for _, g := range rep.Gaps {
		if g.Kind == GapFailedVerification {
			t.Fatalf("gaps = %+v, want the superseded failure left out", rep.Gaps)
		}
	}

	// Without the fresh green run it is the turn's standing outcome again.
	stillRed := evidence.NewLedger()
	stillRed.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven, Paths: []string{"window.go"},
	})
	stillRed.Record(evidence.Receipt{ToolName: "bash", Success: false, Command: "go test ./...", Verification: evidence.VerificationFailed})
	found := false
	for _, g := range Build(nil, stillRed, nil).Gaps {
		if g.Kind == GapFailedVerification {
			found = true
		}
	}
	if !found {
		t.Fatal("a check that stands failed after the change must still be a gap")
	}
}

// A probe written to a scratch directory is not the work product. Reported as
// a change it asks the turn for a check and a review of a file the user will
// never see — the whole "did nothing to the repository" answer, called partial.
func TestScratchWritesOutsideTheWorkspaceAreNotChanges(t *testing.T) {
	scratch := filepath.Join(os.TempDir(), "session-abc", "probe", "main.go")
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{scratch},
		Created:          []string{scratch},
	})
	// Build hands the filter what the ledger stored, so the filter compares in the
	// ledger's path identity. A literal "/tmp/" prefix matched nothing on Windows,
	// where the same path is recorded with its separators flipped.
	scratchRoot := evidence.NormalizePath(os.TempDir())
	inWorkspace := func(path string) bool { return !strings.HasPrefix(path, scratchRoot) }

	rep := Build(nil, ledger, inWorkspace)
	if len(rep.Changes) != 0 || rep.Mutations != 0 {
		t.Fatalf("changes = %+v, mutations = %d, want a scratch write to count as neither", rep.Changes, rep.Mutations)
	}
	if kinds := gapKinds(rep); len(kinds) != 0 {
		t.Fatalf("gap kinds = %v, want none for a turn that changed nothing", kinds)
	}
	if kept := Build(nil, ledger, nil); len(kept.Changes) != 1 {
		t.Fatalf("changes = %+v, want a nil filter to keep every path", kept.Changes)
	}
}

// A turn that changed nothing and claimed nothing delivered nothing the host
// can grade, however many checks it ran on the way. Calling that "done" is
// what turns an honest "I could not reproduce it, send me the caller" into a
// false completion in any score keyed on the verdict.
func TestDiagnosisWithoutDeliveryIsUnknown(t *testing.T) {
	rep := Build(nil, ledgerOf(
		read("counter.go"),
		ran("go test ./...", true),
		ran("go test -race ./...", true),
	), nil)
	if rep.Verdict != VerdictUnknown {
		t.Fatalf("verdict = %v, want unknown: nothing changed and nothing was claimed", rep.Verdict)
	}
	if len(rep.Verifications) == 0 {
		t.Fatal("the checks must still be recorded; the verdict is about delivery, not about hiding them")
	}
}

// The same shape with one mutation is a real delivery, and a clean check over
// it still earns done — the unknown verdict must not swallow ordinary work.
func TestDeliveredAndCheckedIsStillDone(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("counter.go"),
		read("counter.go"),
		ran("go test ./...", true),
	), nil)
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v, want done; gaps %v", rep.Verdict, gapKinds(rep))
	}
}

// A turn that ran its own check and had it declined must not be told that
// nothing ran. The phrase alone reads as false to it, and the command the host
// declined is the one thing that makes the gap actionable instead of puzzling.
func TestUnverifiedChangeNamesTheDeclinedCheck(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("calc.py"),
		read("calc.py"),
		evidence.Receipt{
			ToolName: "bash", Success: true, OutputBytes: 8,
			Command:      `python3 -c "import calc; assert calc.add(2,3)==5; print('ok')"`,
			Verification: evidence.VerificationNotVerification,
		},
	), nil)
	detail := gapDetail(rep, "unverified_change")
	if !strings.Contains(detail, "python3 -c") {
		t.Fatalf("detail = %q, want the declined command named", detail)
	}
	if !strings.Contains(detail, "does not read it as a check") {
		t.Fatalf("detail = %q, want the reason the command did not count", detail)
	}
}

// With no declined candidate the gap says only what it always said: there is
// nothing to name, and inventing a detail would be worse than the phrase.
func TestUnverifiedChangeStaysBareWithNoCandidate(t *testing.T) {
	rep := Build(nil, ledgerOf(wrote("calc.py")), nil)
	if got := gapDetail(rep, "unverified_change"); got != "" {
		t.Fatalf("detail = %q, want none when no command was declined", got)
	}
}

// The honest exit has to end somewhere other than "done". A turn that ran
// conclude_blocked, satisfied every check it declared, and still could not do
// what was asked is not finished work, and a score keyed on the verdict would
// otherwise read its honesty as a completion claim.
func TestBlockedContractIsNotDone(t *testing.T) {
	c := taskcontract.New("fix derive_token so the suite passes")
	c.MarkBlocked()
	rep := Build(c, ledgerOf(
		wrote("apptoken.py"),
		read("apptoken.py"),
		ran("python3 -m pytest", true),
	), nil)
	if rep.Verdict == VerdictDone {
		t.Fatalf("verdict = %v, want anything but done for a turn that declared itself blocked", rep.Verdict)
	}
	if rep.Verdict != VerdictIncomplete {
		t.Fatalf("verdict = %v, want incomplete", rep.Verdict)
	}
}

// A page the host watched pass after the change proves it the way a command
// would; the card must not call the change unverified while readiness is met.
func TestBuildCountsAHostClassifiedBrowserCheck(t *testing.T) {
	check := evidence.Receipt{ToolName: "browser_act", Success: true, Verification: evidence.VerificationPassed, OutputBytes: 64}
	rep := Build(nil, ledgerOf(wrote("index.html"), check), nil)
	if slices.Contains(gapKinds(rep), GapUnverifiedChange.String()) {
		t.Fatalf("gaps = %v: a passing browser check left the change unverified", gapKinds(rep))
	}
	// Unclassified, a browser call says nothing either way.
	rep = Build(nil, ledgerOf(wrote("index.html"), evidence.Receipt{ToolName: "browser_act", Success: true}), nil)
	if !slices.Contains(gapKinds(rep), GapUnverifiedChange.String()) {
		t.Fatalf("gaps = %v: an unclassified browser call counted as a check", gapKinds(rep))
	}
}
