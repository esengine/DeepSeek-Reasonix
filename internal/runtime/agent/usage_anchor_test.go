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
		ContextWindow: anchorWindow, CompactRatio: 0.85, RecentKeep: 2, WorkspaceID: "ws", ModelRef: "m",
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
	a, prov := newAnchorAgent(t)
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
	if a.ContextUsedTokens() >= a.CompactTrigger() {
		t.Fatal("setup: local estimate must still read low after the usage")
	}
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("summary calls = %d; provider reported 92k against an 85k trigger and the fold did not run", prov.calls.Load())
	}
}

func TestLocalEstimateAloneDoesNotFoldWithoutProviderUsage(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, nil)
	foldPass(t, a)
	if prov.calls.Load() != 0 {
		t.Fatalf("summary calls = %d, want 0 with no reported usage", prov.calls.Load())
	}
}

func TestProviderUsageBelowTriggerPlusGrowthFoldsOnlyOnceGrowthCrosses(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 80_000})
	foldPass(t, a)
	if prov.calls.Load() != 0 {
		t.Fatalf("80k reported plus nothing new folded: calls=%d", prov.calls.Load())
	}
	grown := append(a.Session().Messages, provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("g", 30_000)})
	a.Session().Messages = grown
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("80k reported plus ~7k new content must cross 85k: calls=%d", prov.calls.Load())
	}
}

func TestFoldInvalidatesReportedUsageUntilNextUsageArrives(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("setup: calls=%d", prov.calls.Load())
	}
	visible := a.window().modelVisibleMessages()
	if _, ok := a.window().providerReportedFloor(a.window().visibleRequestShape(visible), a.window().currentProjectionVersion()); ok {
		t.Fatal("the pre-fold 92k still reads as the current context after the fold")
	}
	foldPass(t, a)
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("stale pre-fold usage re-triggered the fold: calls=%d", prov.calls.Load())
	}

	sendWithUsage(t, a, &provider.Usage{PromptTokens: 30_000})
	if floor, ok := a.window().providerReportedFloor(a.window().visibleRequestShape(a.window().modelVisibleMessages()), a.window().currentProjectionVersion()); !ok || floor < 30_000 {
		t.Fatalf("post-fold usage did not re-anchor: floor=%d ok=%v", floor, ok)
	}
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("a post-fold 30k folded again: calls=%d", prov.calls.Load())
	}
}

func TestUnreliableUsageAnchorsNothing(t *testing.T) {
	for name, usage := range map[string]*provider.Usage{
		"estimated by the host":    {PromptTokens: 92_000, Estimated: true},
		"provider ran own tools":   {PromptTokens: 92_000, ServerToolRequests: 1},
		"provider reports nothing": {},
	} {
		t.Run(name, func(t *testing.T) {
			a, prov := newAnchorAgent(t)
			sendWithUsage(t, a, usage)
			foldPass(t, a)
			if prov.calls.Load() != 0 {
				t.Fatalf("folded on a usage that is not the provider's own count of our request: calls=%d", prov.calls.Load())
			}
		})
	}
}

func TestReportedUsageCountsCachedTokens(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000, CacheHitTokens: 87_000, CacheMissTokens: 5_000})
	foldPass(t, a)
	if prov.calls.Load() != 1 {
		t.Fatalf("cached tokens occupy the window and must count: calls=%d", prov.calls.Load())
	}
}

func TestLatestAttemptUsageWinsOverBillableAggregate(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 200_000, ContextPromptTokens: 50_000})
	foldPass(t, a)
	if prov.calls.Load() != 0 {
		t.Fatalf("a retry aggregate stood in for the last request's size: calls=%d", prov.calls.Load())
	}
}

func TestSetSessionDropsReportedUsage(t *testing.T) {
	a, prov := newAnchorAgent(t)
	sendWithUsage(t, a, &provider.Usage{PromptTokens: 92_000})
	a.SetSession(anchorSession(t))
	foldPass(t, a)
	if prov.calls.Load() != 0 {
		t.Fatalf("usage of the replaced transcript survived the swap: calls=%d", prov.calls.Load())
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
	}}, Options{ContextWindow: anchorWindow, CompactRatio: 0.85, RecentKeep: 2, WorkspaceID: "ws", ModelRef: "m"}, event.Discard)
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
	}}, Options{ContextWindow: anchorWindow, CompactRatio: 0.85, RecentKeep: 2, WorkspaceID: "ws", ModelRef: "m"}, event.Discard)
	for range 5 {
		sendWithUsage(t, a, &provider.Usage{PromptTokens: 90_000})
		foldPass(t, a)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("summary calls = %d with nothing foldable", prov.calls.Load())
	}
}
