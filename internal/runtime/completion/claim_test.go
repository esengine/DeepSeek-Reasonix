package completion

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/safety/evidence"
)

func claimed(status string, completion string) evidence.Receipt {
	args := `{"status":"` + status + `","completion":` + completion + `}`
	return evidence.Receipt{ToolName: "update_goal", Success: true, Args: []byte(args)}
}

func TestClaimedVerificationThatNeverRanIsUnbacked(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if got := gapKinds(rep); !slices.Contains(got, "unbacked_claim") {
		t.Fatalf("gap kinds = %v, want the fabricated verification caught", got)
	}
	if !strings.Contains(rep.Gaps[0].Detail, "no run of it was recorded") {
		t.Fatalf("gap detail = %q, want the reason named", rep.Gaps[0].Detail)
	}
	if rep.Gaps[0].Kind != GapUnbackedClaim {
		t.Fatalf("an unbacked claim must lead the gap list, got %v", rep.Gaps[0].Kind)
	}
}

func TestClaimedVerificationThatFailedIsUnbacked(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		ran("go test ./...", false),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if !strings.Contains(rep.Gaps[0].Detail, "its last run failed") {
		t.Fatalf("gap = %+v, want the failed run named", rep.Gaps[0])
	}
}

func TestClaimedVerificationBeforeTheLatestChangeIsUnbacked(t *testing.T) {
	rep := Build(nil, ledgerOf(
		ran("go test ./...", true),
		wrote("parser.go"),
		read("parser.go"),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if !strings.Contains(rep.Gaps[0].Detail, "before the latest change") {
		t.Fatalf("gap = %+v, want the stale claim named", rep.Gaps[0])
	}
}

func TestBackedClaimAddsNoGap(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		ran("go test ./...", true),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none: the claim matches a fresh successful receipt", rep.Gaps)
	}
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v, want done", rep.Verdict)
	}
	if len(rep.Claimed.Verified) != 1 {
		t.Fatalf("claim not recorded: %+v", rep.Claimed)
	}
}

// A cited command need not be byte-identical to the one that ran; the shared
// segment matcher decides, exactly as complete_step does.
func TestClaimMatchesTheCommandAsItActuallyRan(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		ran("cd /repo && go test ./...", true),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none: a cd prefix is not a different command", rep.Gaps)
	}
}

// update_goal promises the turn that what it declares unverified never counts
// against it. A report that downgraded the honest answer would be teaching the
// next turn to declare nothing, so the declaration is kept and shown, beside
// the risks rather than among the host's findings.
func TestDeclaredUnverifiedAndRisksAreKeptWithoutCostingTheVerdict(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		ran("go test ./...", true),
		claimed("complete", `{"unverified":["desktop UI never exercised"],"risks":["the migration is one-way"]}`),
	), nil)
	if got := gapKinds(rep); len(got) != 0 {
		t.Fatalf("gap kinds = %v, want a declaration to be no gap at all", got)
	}
	if rep.Verdict != VerdictDone {
		t.Fatalf("verdict = %v, want done: saying so cost the turn its verdict", rep.Verdict)
	}
	if !slices.Equal(rep.Unverified, []string{"desktop UI never exercised"}) {
		t.Fatalf("unverified = %v, want the declaration carried through", rep.Unverified)
	}
	if !slices.Equal(rep.Risks, []string{"the migration is one-way"}) {
		t.Fatalf("risks = %v", rep.Risks)
	}
}

// The invariant that makes the claim safe to accept at all.
func TestClaimCannotClearAHostFoundGap(t *testing.T) {
	receipts := []evidence.Receipt{wrote("parser.go")}
	bare := Build(nil, ledgerOf(receipts...), nil)
	withClaim := Build(nil, ledgerOf(append(receipts,
		claimed("complete", `{"verified":[],"unverified":["ran out of time"],"risks":[]}`))...), nil)

	if len(withClaim.Gaps) < len(bare.Gaps) {
		t.Fatalf("a claim removed a host-found gap: %d -> %d", len(bare.Gaps), len(withClaim.Gaps))
	}
	if !slices.Contains(gapKinds(withClaim), "unreviewed_change") {
		t.Fatalf("gap kinds = %v, want the host's own finding intact", gapKinds(withClaim))
	}
}

func TestFailedUpdateGoalClaimsNothing(t *testing.T) {
	rejected := evidence.Receipt{
		ToolName: "update_goal", Success: false,
		Args: []byte(`{"status":"complete","completion":{"verified":["go test ./..."]}}`),
	}
	rep := Build(nil, ledgerOf(wrote("parser.go"), read("parser.go"), rejected), nil)
	if !rep.Claimed.Empty() {
		t.Fatalf("a rejected update_goal must claim nothing, got %+v", rep.Claimed)
	}
	if got := gapKinds(rep); slices.Contains(got, "unbacked_claim") {
		t.Fatalf("gap kinds = %v, want no claim gap from a failed call", got)
	}
}

func TestLatestClaimWins(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		claimed("continue", `{"unverified":["nothing run yet"]}`),
		ran("go test ./...", true),
		claimed("complete", `{"verified":["go test ./..."]}`),
	), nil)
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want the superseded claim dropped", rep.Gaps)
	}
}
