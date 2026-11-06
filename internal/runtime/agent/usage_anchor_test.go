package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type summaryCounter struct{ calls atomic.Int32 }

func (p *summaryCounter) Name() string { return "summary-counter" }

func (p *summaryCounter) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Summary\nSUMMARY: earlier work, condensed."}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func tinyImage(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

const anchorWindow = 100_000 // fold at 85_000, physical ceiling just under 100_000

// anchorSession is ~280k characters (~70k cold tokens) over many turns. The
// image keeps calibration frozen, so the local estimate stays at the cold rate,
// the shape of the session in #12119.
func anchorSession(t *testing.T) *sessionstore.Session {
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "system"}}
	for i := range 14 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "question " + strings.Repeat("q", 100)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 20_000)},
		)
		if i == 0 {
			msgs = append(msgs, provider.Message{Role: provider.RoleTool, Content: "screenshot", Images: []string{tinyImage(t)}})
		}
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "current"})
	return &sessionstore.Session{Messages: msgs}
}

func newAnchorAgent(t *testing.T) (*Agent, *summaryCounter) {
	prov := &summaryCounter{}
	a := New(prov, tool.NewRegistry(), anchorSession(t), Options{
		ContextWindow: anchorWindow, CompactRatio: 0.85, WorkspaceID: "ws", ModelRef: "m", ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	return a, prov
}

// sendWithUsage runs the real round: build the request, then receive usage for it.
func sendWithUsage(t *testing.T, a *Agent, usage *provider.Usage) {
	t.Helper()
	if _, err := a.prepareSamplingRequest(context.Background()); err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	if usage != nil {
		a.storeLatestRequestUsage(usage)
	}
}

func foldPass(t *testing.T, a *Agent) {
	t.Helper()
	if _, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
}

func TestProviderReportedUsageTriggersFoldWhenLocalEstimateRunsLow(t *testing.T) {
	a, _ := newAnchorAgent(t)
	if got, trigger := a.ContextUsedTokens(), a.CompactTrigger(); got >= trigger {
		t.Fatalf("setup: local estimate %d must sit below the trigger %d", got, trigger)
	}
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000, TotalTokens: 92_100})
	if shape := a.window().visibleRequestShape(a.window().modelVisibleMessages()); shape.imageTokens == 0 {
		t.Fatal("setup: the request must carry an image, which is what keeps calibration frozen")
	}
	if a.sess.output.promptCalibration.Load() != nil {
		t.Fatal("setup: an image request must not calibrate")
	}
	if a.window().estimatedVisibleRequestTokens(a.window().modelVisibleMessages()) >= a.CompactTrigger() {
		t.Fatal("setup: local estimate must still read low after the usage")
	}
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("summary calls = %d; provider reported 92k against an 85k trigger and the fold did not run", archivedFolds(a))
	}
}

func TestLocalEstimateAloneDoesNotFoldWithoutProviderUsage(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, nil)
	foldPass(t, a)
	if archivedFolds(a) != 0 {
		t.Fatalf("summary calls = %d, want 0 with no reported usage", archivedFolds(a))
	}
}

func TestProviderUsageBelowTriggerPlusGrowthFoldsOnlyOnceGrowthCrosses(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 80_000})
	foldPass(t, a)
	if archivedFolds(a) != 0 {
		t.Fatalf("80k reported plus nothing new folded: calls=%d", archivedFolds(a))
	}
	grown := append(a.Session().Messages, provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("g", 30_000)})
	a.Session().Messages = grown
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("80k reported plus ~7k new content must cross 85k: calls=%d", archivedFolds(a))
	}
}

func TestFoldInvalidatesReportedUsageUntilNextUsageArrives(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("setup: calls=%d", archivedFolds(a))
	}
	visible := a.window().modelVisibleMessages()
	if _, ok := a.window().providerReportedFloor(a.window().visibleRequestShape(visible), a.window().currentProjectionVersion()); ok {
		t.Fatal("the pre-fold 92k still reads as the current context after the fold")
	}
	foldPass(t, a)
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("stale pre-fold usage re-triggered the fold: calls=%d", archivedFolds(a))
	}

	sendWithUsage(t, a, &provider.Usage{PromptTokens: 30_000})
	if floor, ok := a.window().providerReportedFloor(a.window().visibleRequestShape(a.window().modelVisibleMessages()), a.window().currentProjectionVersion()); !ok || floor < 30_000 {
		t.Fatalf("post-fold usage did not re-anchor: floor=%d ok=%v", floor, ok)
	}
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("a post-fold 30k folded again: calls=%d", archivedFolds(a))
	}
}

func TestUnreliableUsageAnchorsNothing(t *testing.T) {
	for name, usage := range map[string]*provider.Usage{
		"estimated by the host":    {PromptTokens: 92_000, Estimated: true},
		"provider ran own tools":   {PromptTokens: 92_000, ServerToolRequests: 1},
		"provider reports nothing": {},
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := newAnchorAgent(t)
			sendWithUsage(t, a, usage)
			foldPass(t, a)
			if archivedFolds(a) != 0 {
				t.Fatalf("folded on a usage that is not the provider's own count of our request: calls=%d", archivedFolds(a))
			}
		})
	}
}

func TestReportedUsageCountsCachedTokens(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000, CacheHitTokens: 87_000, CacheMissTokens: 5_000})
	foldPass(t, a)
	if archivedFolds(a) != 1 {
		t.Fatalf("cached tokens occupy the window and must count: calls=%d", archivedFolds(a))
	}
}

func TestLatestAttemptUsageWinsOverBillableAggregate(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 200_000, ContextPromptTokens: 50_000})
	foldPass(t, a)
	if archivedFolds(a) != 0 {
		t.Fatalf("a retry aggregate stood in for the last request's size: calls=%d", archivedFolds(a))
	}
}

func TestSetSessionDropsReportedUsage(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	a.SetSession(anchorSession(t))
	foldPass(t, a)
	if archivedFolds(a) != 0 {
		t.Fatalf("usage of the replaced transcript survived the swap: calls=%d", archivedFolds(a))
	}
}

func TestNewAgentStartsWithoutReportedUsage(t *testing.T) {
	a, _ := newAnchorAgent(t)
	if a.sess.output.usageAnchor.Load() != nil {
		t.Fatal("a freshly built agent (model switch) inherited an anchor")
	}
}

func TestReportedUsageNeverRaisesTheHardCeilingVerdict(t *testing.T) {
	prov := &summaryCounter{}
	a := New(prov, tool.NewRegistry(), &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "only turn"},
	}}, Options{ContextWindow: anchorWindow, CompactRatio: 0.85, WorkspaceID: "ws", ModelRef: "m"}, event.Discard)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 99_990})
	_, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure})
	if errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("a provider-reported size we cannot fold must not end the turn: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnfoldableReportedUsageDoesNotRepeatTheAttempt(t *testing.T) {
	prov := &summaryCounter{}
	a := New(prov, tool.NewRegistry(), &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "only turn"},
	}}, Options{ContextWindow: anchorWindow, CompactRatio: 0.85, WorkspaceID: "ws", ModelRef: "m", ArchiveDir: testenv.TempDir(t)}, event.Discard)
	for range 5 {
		sendWithUsage(t, a, &provider.Usage{PromptTokens: 90_000})
		foldPass(t, a)
	}
	if archivedFolds(a) != 0 {
		t.Fatalf("summary calls = %d with nothing foldable", archivedFolds(a))
	}
}

func TestGaugeReadsTheProviderReportedSizeWhenTheEstimateRunsLow(t *testing.T) {
	a, _ := newAnchorAgent(t)
	before := a.ContextUsedTokens()
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	if got := a.ContextUsedTokens(); got < 92_000 {
		t.Fatalf("gauge = %d after the provider reported 92000 (estimate before: %d); it reads lower than the size the fold trigger acts on", got, before)
	}
	if budget := a.ContextBudget(); budget.TokensUsed < 92_000 {
		t.Fatalf("the budget the model is told counts %d used, below the 92000 the provider reported", budget.TokensUsed)
	}
}

func TestGaugeFallsBackToTheEstimateAfterAFold(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	foldPass(t, a)
	if got := a.ContextUsedTokens(); got >= 92_000 {
		t.Fatalf("gauge = %d after a fold: the pre-fold 92000 describes a context that no longer exists", got)
	}
}

// A session that has sent an image never calibrates, so its estimate stays at
// the cold rate for good. When the provider counts fewer tokens than that guess,
// the gauge must follow the provider (report FB-112W-8A74: 313k shown for a
// 245k context), not the larger of the two.
func TestGaugeFollowsTheProviderWhenTheColdEstimateRunsHigh(t *testing.T) {
	a, _ := newAnchorAgent(t)
	cold := a.ContextUsedTokens()
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 52_000})
	if a.sess.output.promptCalibration.Load() != nil {
		t.Fatal("setup: an image request must not calibrate, which is what leaves the estimate cold")
	}
	if cold <= 56_000 {
		t.Fatalf("setup: the cold estimate %d must sit well above the provider's 52k", cold)
	}
	if got := a.ContextUsedTokens(); got < 52_000 || got > 56_000 {
		t.Fatalf("gauge = %d, want the provider's 52k plus what grew since (cold guess was %d)", got, cold)
	}
	grown := append(a.Session().Messages, provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("g", 8_000)})
	a.Session().Messages = grown
	a.sess.output.contextUsage.Store(nil)
	if got := a.ContextUsedTokens(); got <= 52_000 {
		t.Fatalf("gauge = %d after new content, want it above the anchored 52k", got)
	}
}

// Calibrated, the estimate can still run high (more of the text is CJK than when
// the ratio was learned, and the excess is priced at the cold rate). The provider's
// count wins there too.
func TestGaugeFollowsTheProviderWhenACalibratedEstimateRunsHigh(t *testing.T) {
	a := newCalibratedAgent(t)
	if a.sess.output.promptCalibration.Load() == nil {
		t.Fatal("setup: a text-only request must calibrate")
	}
	est := a.window().estimatedVisibleRequestTokens(a.window().modelVisibleMessages())
	a.sess.output.usageAnchor.Store(&usageAnchor{
		promptTokens: est * 3 / 4, visible: a.window().visibleRequestShape(a.window().modelVisibleMessages()), projectionVersion: a.window().currentProjectionVersion(),
	})
	a.sess.output.contextUsage.Store(nil)
	if got := a.ContextUsedTokens(); got != est*3/4 {
		t.Fatalf("gauge = %d, want the provider's %d, not the estimate %d", got, est*3/4, est)
	}
}

func newCalibratedAgent(t *testing.T) *Agent {
	t.Helper()
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "system"}}
	for range 6 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "question " + strings.Repeat("q", 100)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 4_000)},
		)
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "current"})
	a := New(&summaryCounter{}, tool.NewRegistry(), &sessionstore.Session{Messages: msgs}, Options{
		ContextWindow: anchorWindow, CompactRatio: 0.85, WorkspaceID: "ws", ModelRef: "m", ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 9_000})
	return a
}

// The gauge and the fold trigger read the same number: what the gauge shows is
// what the trigger compares to its line, so a fold never fires while the gauge
// is short of it, nor holds off while the gauge is past it.
func TestGaugeAndFoldTriggerReadOnePressure(t *testing.T) {
	cases := []struct {
		name     string
		reported int // provider prompt tokens; 0 = no anchor
		fold     bool
	}{
		{"anchor above the line folds", 92_000, true},
		{"anchor below the line holds even though the cold estimate is above it", 52_000, false},
		{"no anchor leaves the estimate, below the line", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newAnchorAgent(t)
			if tc.reported > 0 {
				sendWithUsage(t, a, &provider.Usage{PromptTokens: tc.reported})
			} else {
				sendWithUsage(t, a, nil)
			}
			visible := a.window().modelVisibleMessages()
			shape := a.window().visibleRequestShape(visible)
			pressure := a.window().promptPressure(shape, a.window().currentProjectionVersion(), a.window().estimatedShapeTokens(shape))
			if gauge := a.ContextUsedTokens(); gauge != pressure {
				t.Fatalf("gauge %d != the trigger's pressure %d", gauge, pressure)
			}
			foldPass(t, a)
			if folded := archivedFolds(a) == 1; folded != tc.fold {
				t.Fatalf("folded=%v at pressure %d against a line of %d, want %v", folded, pressure, a.CompactTrigger(), tc.fold)
			}
			if tc.fold != (pressure >= a.CompactTrigger()) {
				t.Fatalf("pressure %d against line %d disagrees with the expected fold=%v", pressure, a.CompactTrigger(), tc.fold)
			}
		})
	}
}

// After a fold the reported size describes a context that no longer exists, so
// both readings fall back to the estimate together.
func TestGaugeAndFoldTriggerAgreeAfterAFold(t *testing.T) {
	a, _ := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	foldPass(t, a)
	visible := a.window().modelVisibleMessages()
	shape := a.window().visibleRequestShape(visible)
	est := a.window().estimatedShapeTokens(shape)
	if got := a.ContextUsedTokens(); got != a.window().promptPressure(shape, a.window().currentProjectionVersion(), est) || got >= 92_000 {
		t.Fatalf("after a fold gauge = %d, want the estimate (%d), not the stale 92k", got, est)
	}
}
