package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// recordingSharedWindowProvider logs every request before delegating to the
// shared-window fake, so tests can assert each summary request stayed
// admissible.
type recordingSharedWindowProvider struct {
	sharedWindowTestProvider
	requests []provider.Request
}

func (p *recordingSharedWindowProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return p.sharedWindowTestProvider.Stream(ctx, req)
}

// TestManualCompactEscapesOverCeilingDeadlock reproduces #9059: once the
// model-visible view sits at or above the hard input ceiling, a manual compact
// must still issue an admissible summary request and fold the view back under
// the ceiling instead of failing forever before the provider call.
func TestManualCompactEscapesOverCeilingDeadlock(t *testing.T) {
	window := 50_000
	prov := &recordingSharedWindowProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	sess := foldableSessionOverForce(6)
	a := agentOverForceWindow(t, prov, sess, window)

	big := strings.Repeat("word ", 400)
	for i := 0; i < 300 && a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) < a.hardInputCeiling(); i++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if est, hard := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture did not reach hard ceiling: %d < %d", est, hard)
	}

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("manual compact over ceiling deadlocked: %v", err)
	}
	if est, hard := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.hardInputCeiling(); est >= hard {
		t.Fatalf("manual compact left view at or above hard ceiling: %d >= %d", est, hard)
	}
	if len(prov.requests) == 0 {
		t.Fatal("manual compact never reached the summarizer")
	}
	maxPrompt := window - a.summaryOutputBudget() - protocolReserveTokens
	for i, req := range prov.requests {
		if got := a.estimatedRequestTokens(req); got > maxPrompt {
			t.Fatalf("summary request %d estimated %d tokens, exceeds admissible %d", i, got, maxPrompt)
		}
	}
	summaries := 0
	for _, m := range a.modelVisibleMessages() {
		if isCompactionSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("projection summaries = %d, want exactly 1", summaries)
	}
}

// TestManualCompactOverCeilingPrunesBeforeSummary pins the rescue order: the
// free view-side prune runs before the first summarizer call, so oversized
// tool results in the never-folded tail shrink without a model round-trip.
func TestManualCompactOverCeilingPrunesBeforeSummary(t *testing.T) {
	window := 50_000
	prov := &recordingSharedWindowProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "dump everything"},
	}
	for i := range 10 {
		callID := "call-dump"
		if i > 0 {
			callID = fmt.Sprintf("call-dump-%d", i)
		}
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: callID, Name: "dump", Arguments: `{}`}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: callID, Name: "dump", Content: strings.Repeat("x", 24_000)},
			provider.Message{Role: provider.RoleUser, Content: "again"},
		)
	}
	sess := &Session{Messages: msgs}
	a := agentOverForceWindow(t, prov, sess, window)
	if est, hard := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture did not reach hard ceiling: %d < %d", est, hard)
	}

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("manual compact over tool-heavy ceiling: %v", err)
	}
	if len(prov.requests) == 0 {
		t.Fatal("manual compact never reached the summarizer")
	}
	prunedSeen := false
	for _, m := range prov.requests[0].Messages {
		if m.Role != provider.RoleTool {
			continue
		}
		if len(m.Content) < 24_000 && strings.Contains(m.Content, toolPruneMarker) {
			prunedSeen = true
		}
	}
	if !prunedSeen {
		t.Fatal("first summary request carried unpruned tool results; prune did not run before the summarizer")
	}
	if est, hard := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.hardInputCeiling(); est >= hard {
		t.Fatalf("manual compact left view at or above hard ceiling: %d >= %d", est, hard)
	}
}

// TestManualCompactMultiBatchWhenSingleFoldInsufficient drives a view several
// times the window: one admissible batch cannot free enough, so the rescue
// loop folds again with the prior digest merged into the next input.
func TestManualCompactMultiBatchWhenSingleFoldInsufficient(t *testing.T) {
	window := 50_000
	prov := &recordingSharedWindowProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	sess := foldableSessionOverForce(6)
	a := agentOverForceWindow(t, prov, sess, window)

	big := strings.Repeat("word ", 400)
	hard := a.hardInputCeiling()
	for i := 0; i < 500 && a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) < 2*hard+10_000; i++ {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if est := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()); est < 2*hard {
		t.Fatalf("fixture did not reach double ceiling: %d < %d", est, 2*hard)
	}

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("multi-batch manual compact: %v", err)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("summary calls = %d, want at least 2 rescue batches", len(prov.requests))
	}
	maxPrompt := window - a.summaryOutputBudget() - protocolReserveTokens
	for i, req := range prov.requests {
		if got := a.estimatedRequestTokens(req); got > maxPrompt {
			t.Fatalf("summary request %d estimated %d tokens, exceeds admissible %d", i, got, maxPrompt)
		}
	}
	var second strings.Builder
	for _, m := range prov.requests[1].Messages {
		second.WriteString(m.Content)
	}
	if !strings.Contains(second.String(), summaryTagOpen) {
		t.Fatal("second rescue batch did not merge the prior digest into its input")
	}
	if est := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()); est >= hard {
		t.Fatalf("multi-batch manual compact left view over ceiling: %d >= %d", est, hard)
	}
	summaries := 0
	for _, m := range a.modelVisibleMessages() {
		if isCompactionSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("projection summaries = %d, want exactly 1", summaries)
	}
}

// TestManualCompactBelowCeilingKeepsSingleFold pins the ordinary manual
// contract: below the hard ceiling a manual compact still prunes nothing,
// makes exactly one summarizer call, and installs exactly one projection.
func TestManualCompactBelowCeilingKeepsSingleFold(t *testing.T) {
	prov := &recordingSharedWindowProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	sess := foldableSessionOverForce(60)
	a := agentOverForceWindow(t, prov, sess, 50_000)
	before := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	if before >= a.hardInputCeiling() || before < a.compactTrigger() {
		t.Fatalf("fixture est = %d, want between trigger %d and hard %d", before, a.compactTrigger(), a.hardInputCeiling())
	}

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("manual compact below ceiling: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("summary calls = %d, want exactly 1", prov.calls)
	}
	if got := a.currentProjectionVersion(); got != 1 {
		t.Fatalf("projection version = %d, want exactly 1 (single fold, no prune)", got)
	}
	visible := a.modelVisibleMessages()
	if last := visible[len(visible)-1]; last.Role != provider.RoleUser || last.Content != "continue" {
		t.Fatalf("verbatim tail not retained: %+v", last)
	}
}
