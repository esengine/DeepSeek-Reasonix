package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// A fold whose region carries a sentinel only the raw transcript holds, and a
// digest that names one of its two changes and drops the other.
func foldWithADroppedChange() []provider.Message {
	return coverageRegion()
}

const digestNamingOneChange = "## Files & code\n- internal/parser/lexer.go was rewritten"

func repairAgent(t *testing.T, replies []scriptedReply) (*Agent, *scriptedSummarizer) {
	t.Helper()
	prov := &scriptedSummarizer{replies: replies}
	return New(prov, coverageRegistry(), &sessionstore.Session{}, Options{ContextWindow: 200000}, nil), prov
}

// Correcting a digest may consume the digest and the facts the host already
// owns. It may not consume the fold again: re-reading the whole region to
// recover a filename costs what reading it the first time cost.
func TestCoverageCorrectionNeverRereadsTheFold(t *testing.T) {
	a, prov := repairAgent(t, []scriptedReply{
		{text: digestNamingOneChange, usage: billed(9000, 400)},
		{text: "a second answer nobody should have asked for", usage: billed(9500, 450)},
	})
	ctx, spend := withCompactionSpend(context.Background())

	res, _, err := a.window().foldOrDegrade(ctx, CompactionTriggerPressure, false, foldWithADroppedChange(), "", 4000)
	if err != nil {
		t.Fatalf("foldOrDegrade: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: the digest dropped a change and the fold was bought again", prov.calls)
	}
	if got := spend.read(); got.Calls != 1 || got.InputTokens != 9000 {
		t.Errorf("transaction = %+v, want the one call the fold needed", got)
	}
	// And the correction happened: what the digest dropped is in what is installed.
	if !strings.Contains(res.Text, "internal/parser/reader.go") {
		t.Errorf("the dropped change is not in the installed text:\n%s", res.Text)
	}
	if !res.CoverageBackstopped {
		t.Error("the host completed the digest without recording that it had to")
	}
}

// The correction has to actually close the gap, or it has only made the fold
// cheaper. Measured against the text that gets installed, host block included.
func TestCoverageCorrectionClosesTheGapItFound(t *testing.T) {
	a, _ := repairAgent(t, []scriptedReply{{text: digestNamingOneChange, usage: billed(9000, 400)}})
	fold := foldWithADroppedChange()

	res, tele, err := a.window().foldOrDegrade(context.Background(), CompactionTriggerPressure, false, fold, "", 4000)
	if err != nil {
		t.Fatalf("foldOrDegrade: %v", err)
	}
	if tele.CoverageRequired == 0 {
		t.Fatal("this fold was meant to owe the digest some facts")
	}
	// The digest's own shortfall is still reported — that is what the card reads.
	if tele.CoverageMissing == 0 {
		t.Error("a digest that dropped a change reports nothing missing")
	}
	if installed := measureFoldCoverage(fold, a.toolFactsFor, res.Text); installed.Missing() != 0 {
		t.Errorf("what gets installed still drops %d facts: %s", installed.Missing(), installed.Reason())
	}
}

// The circuit breaker survives the retirement. A digest that named none of the
// fold's changes is a broken summary, and the host block completing the record
// is not a reason to install it — accepting it because the host cleaned up
// would retire the one signal that the summarizer produced nothing usable.
func TestADigestThatNamedNoChangeIsStillRefused(t *testing.T) {
	a, prov := repairAgent(t, []scriptedReply{
		{text: "some work happened", usage: billed(9000, 400)},
		{text: "a second answer nobody should have asked for", usage: billed(9500, 450)},
	})

	_, _, err := a.window().foldOrDegrade(context.Background(), CompactionTriggerPressure, false, foldWithADroppedChange(), "", 4000)
	if err == nil {
		t.Fatal("a digest naming none of the fold's changes was installed")
	}
	if !IsCompactionDeclined(err) {
		t.Fatalf("err = %v, want a declined checkpoint", err)
	}
	// What changed is the price of finding out: refusing used to cost a second
	// reading of the whole fold first.
	if prov.calls != 1 {
		t.Errorf("summarizer calls = %d, want 1: refusing must not buy the fold twice", prov.calls)
	}
}

// Past what the host block can hold, the digest is not completable and the
// checkpoint is refused rather than installed with a silent shortfall.
func TestADigestBeyondTheBackstopIsRefused(t *testing.T) {
	var calls []provider.ToolCall
	var region []provider.Message
	for i := range maxBackstopFacts + 5 {
		id := fmt.Sprintf("w%d", i)
		calls = append(calls, provider.ToolCall{ID: id, Name: "write_file",
			Arguments: fmt.Sprintf(`{"path":"internal/gen/f%02d.go"}`, i)})
	}
	region = append(region, provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	for _, c := range calls {
		region = append(region, provider.Message{Role: provider.RoleTool, ToolCallID: c.ID, Name: "write_file", Content: "wrote"})
	}

	a, prov := repairAgent(t, []scriptedReply{{text: "did some work", usage: billed(9000, 400)}})
	_, _, err := a.window().foldOrDegrade(context.Background(), CompactionTriggerPressure, false, region, "", 4000)
	if err == nil {
		t.Fatal("a digest carrying none of 25 changes was installed")
	}
	if !IsCompactionDeclined(err) {
		t.Fatalf("err = %v, want a declined checkpoint", err)
	}
	if prov.calls != 1 {
		t.Errorf("summarizer calls = %d, want 1: refusing must not cost a second reading", prov.calls)
	}
}

// A fold that is the only way out is never refused: there is nowhere else to go.
func TestAFoldThatMustFreeIsNeverRefused(t *testing.T) {
	a, _ := repairAgent(t, []scriptedReply{{text: "did some work", usage: billed(9000, 400)}})
	if _, _, err := a.window().foldOrDegrade(context.Background(), CompactionTriggerOverflow, true, foldWithADroppedChange(), "", 4000); err != nil {
		t.Fatalf("a must-free fold was refused: %v", err)
	}
}
