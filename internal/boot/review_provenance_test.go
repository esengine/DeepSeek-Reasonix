package boot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// Evidence that moves a durable decision has to be attributable to the run that
// produced it. Both surfaces reach the same reviewer with the same report; only
// one of them runs inside the delegation lifecycle, and only that one can prove
// anything. The other still reports — a block is worth having from a run that
// proves nothing — it just cannot close an obligation.
func TestOnlyAnAttributableRunCanCloseAReviewObligation(t *testing.T) {
	for _, tc := range []struct {
		surface      string
		attributable bool
	}{
		{"run_skill", true},
		{"read_only_skill", false},
	} {
		t.Run(tc.surface, func(t *testing.T) {
			parent := runReviewerThrough(t, tc.surface, "pass")

			var report *evidence.Receipt
			for _, r := range parent.Summary().Receipts {
				if r.ToolName == "review_report" && r.Success {
					report = &r
				}
			}
			if report == nil {
				t.Fatal("the reviewer's report never reached the parent ledger")
			}
			// The delivery fact holds either way: the worker handed back what
			// it owed, through whichever surface started it.
			if report.ReportKind != evidence.ReviewKindReview {
				t.Fatalf("report kind = %q", report.ReportKind)
			}

			named := strings.TrimSpace(report.SourceExecutionID) != ""
			if named != tc.attributable {
				t.Fatalf("execution named = %v (%q), want %v", named, report.SourceExecutionID, tc.attributable)
			}
			proves := evidence.ReceiptProves(*report, evidence.ReviewKindReview)
			if proves != tc.attributable {
				t.Fatalf("proves review = %v, want %v", proves, tc.attributable)
			}
			ok, _, _ := parent.HasStructuredReviewAfter(evidence.ReviewKindReview, 0, []string{"a.go"})
			if ok != tc.attributable {
				t.Fatalf("closes the review obligation = %v, want %v", ok, tc.attributable)
			}
		})
	}
}

// The half that survives losing authority: a run that can prove nothing can
// still refuse. A block is a verdict about what the reviewer did look at, so it
// reaches delivery whether or not the host could name the execution.
func TestAnUnattributableRunCanStillBlock(t *testing.T) {
	parent := runReviewerThrough(t, "read_only_skill", "block")
	if _, ok := parent.BlockingReviewAfter(0); !ok {
		t.Fatal("a blocking verdict must reach delivery even from a run that proves nothing")
	}
}

// runReviewerThrough runs the built-in reviewer through one surface and returns
// the parent ledger its receipts merged into.
func runReviewerThrough(t *testing.T, surface, verdict string) *evidence.Ledger {
	t.Helper()
	reviewer, ok := skill.New(skill.Options{}).Read("review")
	if !ok {
		t.Fatal("built-in review is missing")
	}
	reviewer.AllowedTools = nil

	w := newReviewWorldWith(t, &reportingProvider{verdict: verdict}, reviewer)
	parent := evidence.NewLedger()
	parent.Record(evidence.ReceiptFromToolCall("edit_file",
		json.RawMessage(`{"path":"a.go"}`), true, evidence.ToolFacts{}))
	ctx := evidence.WithLedger(w.ctx, parent)

	var err error
	if surface == "run_skill" {
		_, err = w.skills.run(ctx, reviewer, "review it", skill.SubagentRunOptions{})
	} else {
		_, err = w.skills.runReadOnly(ctx, reviewer, "review it", skill.SubagentRunOptions{})
	}
	if err != nil {
		t.Fatalf("%s: %v", surface, err)
	}
	return parent
}

// reportingProvider drives a child that reads a file and then reports on it,
// which is the shortest run that earns a report the host will accept.
type reportingProvider struct {
	mu      sync.Mutex
	turn    int
	verdict string
	kind    string
}

func (*reportingProvider) Name() string      { return "reporting-probe" }
func (*reportingProvider) offered() []string { return nil }

func (p *reportingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.turn++
	turn := p.turn
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	switch turn {
	case 1:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "r1", Name: "matrix_read", Arguments: `{"path":"a.go"}`}}
	case 2:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "r2", Name: "review_report",
			Arguments: `{"kind":"` + p.reportKind() + `","verdict":"` + p.verdict + `","reviewed_paths":["a.go"]}`}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "verdict: " + p.verdict}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// The host knows why the report it holds does not count, so it says so. A gate
// that repeats "run a review" at a turn that just ran one teaches the model a
// rule about batching or about the tool, and that invented rule outlives the
// turn.
func TestTheGateNamesWhyAnUnattributableReportDidNotCount(t *testing.T) {
	parent := runReviewerThrough(t, "read_only_skill", "pass")
	if got := parent.ReviewProofGapFor(evidence.ReviewKindReview); got != evidence.ReviewProofUnattributed {
		t.Fatalf("proof gap = %v, want unattributed", got)
	}
	if got := parent.ReviewProofGapFor(evidence.ReviewKindSecurity); got != evidence.ReviewProofNone {
		t.Fatalf("a kind never reported has no gap to explain, got %v", got)
	}
	durable := runReviewerThrough(t, "run_skill", "pass")
	if got := durable.ReviewProofGapFor(evidence.ReviewKindReview); got != evidence.ReviewProofNone {
		t.Fatalf("a report that proved leaves no gap, got %v", got)
	}
}

func (p *reportingProvider) reportKind() string {
	if p.kind == "" {
		return "review"
	}
	return p.kind
}
