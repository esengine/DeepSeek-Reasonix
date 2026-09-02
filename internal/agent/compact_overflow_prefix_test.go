package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

type overflowSummaryProvider struct {
	requests []provider.Request
}

func (p *overflowSummaryProvider) Name() string { return "overflow-summary" }

func (p *overflowSummaryProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{
		WindowMode: provider.ContextWindowShared,
		LimitMode:  provider.OutputLimitAlways,
	}
}

func (p *overflowSummaryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return chunks(
		provider.Chunk{Type: provider.ChunkText, Text: "compact durable summary"},
		provider.Chunk{Type: provider.ChunkDone},
	), nil
}

func TestOverflowSummarizesLargestAdmissibleContiguousPrefix(t *testing.T) {
	sess := foldableSessionOverForce(120)
	prov := &overflowSummaryProvider{}
	a := agentOverForceWindow(t, prov, sess, 60_000)
	msgs := sess.Snapshot()
	head, plannedEnd, ok := a.planFoldRegion(msgs, true)
	if !ok {
		t.Fatal("fixture has no foldable prefix")
	}
	safeEnd := a.maximumSafeSummaryPrefixEnd(msgs, head, plannedEnd, "")
	if safeEnd <= head || safeEnd >= plannedEnd {
		t.Fatalf("safe fold end = %d, want a non-empty prefix smaller than planned end %d", safeEnd, plannedEnd)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("overflow recovery: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if got, max := a.estimatedRequestTokens(req), a.effectiveContextWindow()-a.summaryOutputBudget()-protocolReserveTokens; got > max {
		t.Fatalf("summary request tokens = %d, exceeds admissible input %d", got, max)
	}
	if receipt := a.sess.compactionState.LastReceipt; receipt == nil || receipt.CoveredCount != safeEnd {
		t.Fatalf("receipt = %+v, want covered prefix %d", receipt, safeEnd)
	}
	if current := a.contextManager().currentPrepared(); current.InputTokens >= a.hardInputCeiling() {
		t.Fatalf("recovered projection tokens = %d, hard ceiling = %d", current.InputTokens, a.hardInputCeiling())
	}
}

func TestMaximumSafeSummaryPrefixKeepsToolPairsTogether(t *testing.T) {
	// A 10k window leaves 7244 prompt tokens after the scaled 2500-token
	// summary budget and protocol reserve. The initial boundary splits the tool
	// results, so it must retreat across the whole assistant/tool group.
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "old task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: strings.Repeat("first result payload. ", 150)},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: strings.Repeat("second result payload. ", 1500)},
		{Role: provider.RoleUser, Content: "recent task"},
	}
	a := &Agent{
		agentConfig: agentConfig{contextWindow: 10_000},
		svc:         agentServices{prov: &overflowSummaryProvider{}},
		sess:        sessionRuntime{conversation: &Session{Messages: msgs}},
	}
	end := a.maximumSafeSummaryPrefixEnd(msgs, 1, len(msgs)-1, "")
	if end != 2 {
		t.Fatalf("fold boundary = %d, want 2 so the assistant call and both results stay in the tail", end)
	}
}

// opaqueWindowProvider declares no ContextBudgetPolicy (Unknown window mode)
// and is never admitted, reproducing the #9572 shape: a freshly switched-to
// gateway whose summary request used to bypass the safe-prefix cap entirely.
type opaqueWindowProvider struct {
	requests []provider.Request
}

func (p *opaqueWindowProvider) Name() string { return "opaque-window" }

func (p *opaqueWindowProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return chunks(
		provider.Chunk{Type: provider.ChunkText, Text: "compact durable summary"},
		provider.Chunk{Type: provider.ChunkDone},
	), nil
}

func TestPressureSummaryCappedByConfiguredWindowWithoutAdmission(t *testing.T) {
	sess := foldableSessionOverForce(120)
	prov := &opaqueWindowProvider{}
	a := agentOverForceWindow(t, prov, sess, 60_000)
	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("pressure maintenance: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if got, max := a.estimatedRequestTokens(req), a.effectiveContextWindow()-a.summaryOutputBudget()-protocolReserveTokens; got > max {
		t.Fatalf("summary request tokens = %d, exceeds configured-window cap %d", got, max)
	}
}

func TestPressureSummaryCappedByLearnedWindowAfterSessionReset(t *testing.T) {
	sess := foldableSessionOverForce(120)
	prov := &opaqueWindowProvider{}
	a := agentOverForceWindow(t, prov, sess, 60_000)
	a.contextWindow = 0
	a.sess.output.learned.Store(&learnedContextBudget{windowTokens: 60_000})
	a.storeAdmission(contextAdmission{ObservedWindow: 60_000})
	a.SetSession(sess)
	if got := a.lastAdmission().ObservedWindow; got != 0 {
		t.Fatalf("session reset retained observed window %d", got)
	}

	msgs := sess.Snapshot()
	head, plannedEnd, ok := a.planFoldRegion(msgs, true)
	if !ok {
		t.Fatal("fixture has no foldable prefix")
	}
	safeEnd := a.maximumSafeSummaryPrefixEnd(msgs, head, plannedEnd, "")
	if safeEnd <= head || safeEnd >= plannedEnd {
		t.Fatalf("safe fold end = %d, want a non-empty prefix smaller than planned end %d", safeEnd, plannedEnd)
	}
	request := a.summaryRequest(msgs[head:safeEnd], "")
	if got, max := a.estimatedRequestTokens(request), a.effectiveContextWindow()-a.summaryOutputBudget()-protocolReserveTokens; got > max {
		t.Fatalf("summary request tokens = %d, exceeds learned-window cap %d", got, max)
	}
}

func smallWindowPressureSession() *Session {
	return &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("x", 20_000)},
		{Role: provider.RoleAssistant, Content: strings.Repeat("y", 12_000)},
		{Role: provider.RoleUser, Content: "recent"},
	}}
}

func TestPressureSummaryScalesOutputBudgetForSmallWindow(t *testing.T) {
	const window = 10_000
	prov := &opaqueWindowProvider{}
	sess := smallWindowPressureSession()
	a := agentOverForceWindow(t, prov, sess, window)
	est := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	if est < a.compactTrigger() || est >= a.hardInputCeiling() {
		t.Fatalf("fixture tokens = %d, want pressure range [%d, %d)", est, a.compactTrigger(), a.hardInputCeiling())
	}
	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("small-window pressure maintenance: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.MaxTokens != window/4 {
		t.Fatalf("summary max tokens = %d, want scaled budget %d", req.MaxTokens, window/4)
	}
	if got := a.estimatedRequestTokens(req) + req.MaxTokens + protocolReserveTokens; got > window {
		t.Fatalf("summary request total = %d, exceeds small window %d", got, window)
	}
}

func TestPressureNoSafePrefixRecordsBudgetReason(t *testing.T) {
	const window = 10_000
	prov := &opaqueWindowProvider{}
	a := agentOverForceWindow(t, prov, smallWindowPressureSession(), window)

	_, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{
		Trigger: CompactionTriggerPressure, Instructions: strings.Repeat("preserve this focus ", window),
	})
	if err != nil {
		t.Fatalf("pressure maintenance should soft-skip: %v", err)
	}
	if len(prov.requests) != 0 {
		t.Fatalf("summary requests = %d, want none when no prefix fits", len(prov.requests))
	}
	receipt := a.sess.compactionState.LastReceipt
	if receipt == nil || receipt.Status != "blocked" || !strings.Contains(receipt.Reason, "no balanced prefix leaves enough room") {
		t.Fatalf("receipt = %+v, want precise summary-budget reason", receipt)
	}
}
