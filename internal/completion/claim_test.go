package completion

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/evidence"
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
	))
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
	))
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
	))
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
	))
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
	))
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none: a cd prefix is not a different command", rep.Gaps)
	}
}

func TestDeclaredUnverifiedAndRisksSurvive(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		ran("go test ./...", true),
		claimed("complete", `{"unverified":["desktop UI never exercised"],"risks":["the migration is one-way"]}`),
	))
	if got := gapKinds(rep); !slices.Equal(got, []string{"declared_unverified"}) {
		t.Fatalf("gap kinds = %v, want the self-declared gap kept", got)
	}
	if rep.Verdict != VerdictPartial {
		t.Fatalf("verdict = %v, want partial: declaring a gap is honest, not clean", rep.Verdict)
	}
	if !slices.Equal(rep.Risks, []string{"the migration is one-way"}) {
		t.Fatalf("risks = %v", rep.Risks)
	}
}

// The invariant that makes the claim safe to accept at all.
func TestClaimCannotClearAHostFoundGap(t *testing.T) {
	receipts := []evidence.Receipt{wrote("parser.go"), ran("go test ./...", true)}
	bare := Build(nil, ledgerOf(receipts...))
	withClaim := Build(nil, ledgerOf(append(receipts,
		claimed("complete", `{"verified":["go test ./..."],"unverified":[],"risks":[]}`))...))

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
	rep := Build(nil, ledgerOf(wrote("parser.go"), read("parser.go"), rejected))
	if !rep.Claimed.Empty() {
		t.Fatalf("a rejected update_goal must claim nothing, got %+v", rep.Claimed)
	}
	if got := gapKinds(rep); slices.Contains(got, "unbacked_claim") {
		t.Fatalf("gap kinds = %v, want no claim gap from a failed call", got)
	}
}

func TestLatestCompleteClaimIgnoresContinueAndFailedCalls(t *testing.T) {
	led := ledgerOf(
		wrote("parser.go"),
		claimed("continue", `{"unverified":["nothing run yet"]}`),
		claimed("complete", `{"unverified":["real-engine e2e"]}`),
	)
	got, ok := LatestCompleteClaim(led)
	if !ok || len(got.Unverified) != 1 || got.Unverified[0] != "real-engine e2e" {
		t.Fatalf("LatestCompleteClaim = (%+v, %v)", got, ok)
	}
	rejected := evidence.Receipt{
		ToolName: "update_goal", Success: false,
		Args: []byte(`{"status":"complete","completion":{"unverified":["should not count"]}}`),
	}
	if _, ok := LatestCompleteClaim(ledgerOf(rejected)); ok {
		t.Fatal("a failed update_goal must not yield a complete claim")
	}
}

func TestLatestClaimWins(t *testing.T) {
	rep := Build(nil, ledgerOf(
		wrote("parser.go"),
		read("parser.go"),
		claimed("continue", `{"unverified":["nothing run yet"]}`),
		ran("go test ./...", true),
		claimed("complete", `{"verified":["go test ./..."]}`),
	))
	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want the superseded claim dropped", rep.Gaps)
	}
}
