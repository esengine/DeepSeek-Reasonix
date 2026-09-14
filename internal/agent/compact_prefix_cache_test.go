package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestSummaryFoldPlanReusesFullMainRequestBytes pins the cache-alignment fix:
// with frozen main-request bytes, the summarizer prefix is the ENTIRE main
// request (the provider-cached unit), not a head-cropped view. head is pinned
// at 1 (system only), so any head-cropped prefix could never match more than
// the system unit — the historical 16,896-hit symptom. The fold is not
// re-sent; the instruction locates it by anchor excerpts.
func TestSummaryFoldPlanReusesFullMainRequestBytes(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 200000, prov)

	mainReq := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "user one"},
		{Role: provider.RoleAssistant, Content: "assistant one"},
		{Role: provider.RoleUser, Content: "user two"},
		{Role: provider.RoleAssistant, Content: "assistant two"},
		{Role: provider.RoleUser, Content: "user three"},
	}
	a.saveMainRequest(mainReq, nil)

	// Live view drifted: a projection update rewrote the middle; two new
	// turns were appended beyond the frozen bytes.
	view := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "user one"},
		{Role: provider.RoleAssistant, Content: "assistant one"},
		{Role: provider.RoleUser, Content: "SUMMARY: user two + assistant two were folded"},
		{Role: provider.RoleUser, Content: "user three"},
		{Role: provider.RoleUser, Content: "user four"},
		{Role: provider.RoleAssistant, Content: "assistant four"},
		{Role: provider.RoleUser, Content: "user five"},
	}
	head, start := 1, 5 // fold = view[1:5]

	prefix, extra, anchors := a.summaryFoldPlan(view, head, start)
	if len(prefix) != len(mainReq) {
		t.Fatalf("prefix = %d messages, want the whole frozen main request (%d)", len(prefix), len(mainReq))
	}
	for i := range prefix {
		if prefix[i].Role != mainReq[i].Role || prefix[i].Content != mainReq[i].Content {
			t.Fatalf("prefix diverges from main-request bytes at %d", i)
		}
	}
	// Extra carries the fold tail beyond the frozen bytes (view[6:5] is empty
	// here since start=5 <= len(saved)=6), and anchors locate the fold.
	if anchors == "" {
		t.Fatal("anchor instruction missing")
	}
	if !strings.Contains(anchors, "user one") && !strings.Contains(anchors, "assistant one") {
		t.Fatalf("anchor instruction %q does not name fold content", anchors)
	}
	if len(extra) != 0 {
		t.Fatalf("extra = %d messages, want 0 (fold ends inside the frozen bytes)", len(extra))
	}

	// The full request: frozen main bytes + anchor instruction only.
	res, tele, err := a.foldSummaryWithTelemetry(context.Background(), CompactionTriggerManual, prefix, extra, "focus"+anchors, 100, SummaryInputCachePrefix)
	if err != nil {
		t.Fatalf("foldSummaryWithTelemetry: %v", err)
	}
	if res.Text == "" {
		t.Fatal("empty summary")
	}
	if len(prov.got) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.got))
	}
	sent := prov.got[0].Messages
	if len(sent) != len(mainReq)+1 {
		t.Fatalf("sent = %d messages, want main request (%d) + instruction (1)", len(sent), len(mainReq))
	}
	for i := range mainReq {
		if sent[i].Role != mainReq[i].Role || sent[i].Content != mainReq[i].Content {
			t.Fatalf("sent prefix diverges at %d:\n sent=%+v\n main=%+v", i, sent[i], mainReq[i])
		}
	}
	if !strings.Contains(sent[len(sent)-1].Content, "user one") {
		t.Fatal("instruction message does not carry the fold anchor")
	}
	_ = tele
}

// TestSummaryFoldPlanExtendsBeyondFrozenBytes covers new turns appended after
// the main request: their fold-tail messages must be sent as extra.
func TestSummaryFoldPlanExtendsBeyondFrozenBytes(t *testing.T) {
	a := newFoldAgent(t, 200000, &countingProvider{reply: "digest"})
	a.saveMainRequest([]provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
	}, nil)
	view := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
		{Role: provider.RoleUser, Content: "u3-new"},
		{Role: provider.RoleAssistant, Content: "a3-new"},
	}
	prefix, extra, anchors := a.summaryFoldPlan(view, 1, 6)
	if len(prefix) != 4 {
		t.Fatalf("prefix = %d, want 4 (whole frozen request)", len(prefix))
	}
	if len(extra) != 2 || extra[0].Content != "u3-new" || extra[1].Content != "a3-new" {
		t.Fatalf("extra = %+v, want the two new turns", extra)
	}
	if anchors == "" {
		t.Fatal("anchor instruction missing")
	}
}

// TestSummaryFoldPlanFallsBackWithoutMainRequest covers the fresh-resume case:
// no sampling request has been sent in this process, but the live view still
// byte-matches the parent process's last request while the cache is warm — so
// the whole view replays as the prefix (C1 warm-replay shape) and the fold is
// located by anchor excerpts, not re-sent.
func TestSummaryFoldPlanFallsBackWithoutMainRequest(t *testing.T) {
	a := newFoldAgent(t, 200000, &countingProvider{reply: "digest"})
	view := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
	}
	prefix, extra, anchors := a.summaryFoldPlan(view, 1, 3)
	if len(prefix) != len(view) || prefix[0].Content != "system" || prefix[3].Content != "u2" {
		t.Fatalf("fallback prefix = %+v, want the whole view replayed", prefix)
	}
	if len(extra) != 0 {
		t.Fatalf("fallback extra = %d messages, want 0 (whole view is the prefix)", len(extra))
	}
	if anchors == "" || !strings.Contains(anchors, "u1") {
		t.Fatalf("fallback anchors = %q, want fold located by excerpt", anchors)
	}
}

// TestSummaryFoldEstimateClampsBeyondSaved guards the budget planner against
// a restored wire prefix longer than the live view: candidate indices inside
// the frozen bytes must not slice the view out of range (2026-08-31 05:11:
// panic "slice bounds out of range [3:2]" on resume with a sidecar wire).
func TestSummaryFoldEstimateClampsBeyondSaved(t *testing.T) {
	a := newFoldAgent(t, 200000, &countingProvider{reply: "digest"})
	a.saveMainRequest([]provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
	}, nil)
	view := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
	}
	// candidate inside the frozen prefix length: must not panic, extra empty.
	req := a.summaryFoldEstimate(view, 1, 2, "")
	if len(req.Messages) < 3 {
		t.Fatalf("estimate request = %d messages, want frozen prefix present", len(req.Messages))
	}
}

// TestSummaryFoldPlanCropsOverWindowView covers the over-window resume case:
// the whole view cannot be replayed inside the hard ceiling, so the old
// cropped shape (view prefix + full fold) is the only option — a miss, but a
// compaction that still fits.
func TestSummaryFoldPlanCropsOverWindowView(t *testing.T) {
	a := newFoldAgent(t, 4000, &countingProvider{reply: "digest"}) // tiny window
	view := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: strings.Repeat("x ", 30000)},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
	}
	prefix, extra, anchors := a.summaryFoldPlan(view, 1, 3)
	if len(prefix) != 1 || prefix[0].Content != "system" {
		t.Fatalf("cropped prefix = %d messages, want view[:1]", len(prefix))
	}
	if len(extra) != 2 {
		t.Fatalf("cropped extra = %d messages, want view[1:3]", len(extra))
	}
	if anchors != "" {
		t.Fatalf("cropped fallback must not emit anchors, got %q", anchors)
	}
}

// TestCompactionRequestPrefixMatchesLastMainRequest drives the real compaction
// path: a main request freezes wire bytes, the live view drifts (prune /
// projection rewrite), and the summarizer must still send the frozen prefix so
// its request shares the provider-cached unit. A LocalOnly message verifies
// head indexing survives normalization stripping.
func TestCompactionRequestPrefixMatchesLastMainRequest(t *testing.T) {
	const window = 200_000
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("a ", 60000)},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("b ", 60000)},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
		{Role: provider.RoleUser, Content: "local-only note", LocalOnly: true},
		{Role: provider.RoleUser, Content: "g"},
	}}
	prov := &fakeProvider{reply: "s"}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: window, CompactRatio: 0.8, RecentKeep: 2}, event.Discard)

	// 1. Main request: freeze the wire bytes exactly as prepareSamplingRequest
	// does (normalized view, LocalOnly stripped: 9 -> 8 wire messages).
	mainReq := a.normalizeModelRequestMessages(sess.Messages)
	frozenTools := []provider.ToolSchema{
		{Name: "search", Description: "search the web", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	a.saveMainRequest(mainReq, frozenTools)

	// 2. Live view drifts after the main request (projection/prune rewrite).
	sess.Messages[4] = provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("b ", 2000) + " [pruned]"}

	// 3. Compaction runs on the drifted view.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 170000})
	if len(prov.got) == 0 {
		t.Fatal("compaction made no summarizer request")
	}
	sent := prov.got

	// The summary request prefix must be the ENTIRE frozen main request (the
	// provider-cached unit), byte-exact, with the drifted view only feeding
	// the anchor instruction — never the request bytes.
	saved := a.savedMainRequest()
	if saved == nil || len(saved.messages) == 0 {
		t.Fatal("main request bytes were not saved")
	}
	prefix := saved.messages
	if len(sent) != len(prefix)+1 {
		t.Fatalf("summary request = %d messages, want frozen main request (%d) + instruction (1)", len(sent), len(prefix))
	}
	for i := range prefix {
		if sent[i].Role != prefix[i].Role || sent[i].Content != prefix[i].Content {
			t.Fatalf("summary prefix diverges from main-request bytes at %d:\n sent=%+v\n main=%+v", i, sent[i], prefix[i])
		}
	}
	// The summary request must send the FROZEN tool schemas, not the live
	// registry: the server caches system+tools+messages as one unit, and the
	// live tool set can grow (MCP registration) after the main request.
	if len(prov.reqs) == 0 {
		t.Fatal("no provider request captured")
	}
	lastReq := prov.reqs[len(prov.reqs)-1]
	if len(lastReq.Tools) != len(frozenTools) {
		t.Fatalf("summary tools = %d, want frozen %d", len(lastReq.Tools), len(frozenTools))
	}
	for i := range frozenTools {
		if lastReq.Tools[i].Name != frozenTools[i].Name || lastReq.Tools[i].Description != frozenTools[i].Description ||
			string(lastReq.Tools[i].Parameters) != string(frozenTools[i].Parameters) {
			t.Fatalf("summary tool %d diverges from frozen schema: %+v vs %+v", i, lastReq.Tools[i], frozenTools[i])
		}
	}
	// The instruction (last message) locates the fold by anchor excerpts; the
	// fold itself is not re-sent.
	last := sent[len(sent)-1]
	if last.Role != provider.RoleUser || !strings.Contains(last.Content, "segment to summarize") {
		t.Fatalf("last message is not the anchor instruction: %+v", last)
	}
	for _, m := range sent[:len(sent)-1] {
		if strings.Contains(m.Content, "pruned") {
			t.Fatal("fold content was re-sent; the fold lives inside the frozen prefix")
		}
	}
	// The lossless projection inverse: the checkpoint persists the exact
	// prefix this summary request replayed (wire form), so a resumed process
	// re-normalizing it inside summaryRequest gets the same bytes.
	a.sess.compactionMu.Lock()
	persisted := a.sess.compactionState.LastWireMessages
	a.sess.compactionMu.Unlock()
	wantWire := a.normalizeModelRequestMessages(prefix)
	if len(persisted) != len(wantWire) {
		t.Fatalf("sidecar wire prefix = %d messages, want %d", len(persisted), len(wantWire))
	}
	for i := range wantWire {
		if persisted[i].Content != wantWire[i].Content || persisted[i].Role != wantWire[i].Role {
			t.Fatalf("sidecar wire prefix diverges at %d: %q vs %q", i, persisted[i].Content, wantWire[i].Content)
		}
	}
	// Re-normalizing the persisted wire form must be a fixpoint: a resumed
	// process sends normalize(restored) and must reproduce this exact request.
	renorm := a.normalizeModelRequestMessages(persisted)
	if len(renorm) != len(persisted) {
		t.Fatalf("re-normalized wire = %d messages, want %d (fixpoint)", len(renorm), len(persisted))
	}
	for i := range persisted {
		if renorm[i].Content != persisted[i].Content || renorm[i].Role != persisted[i].Role {
			t.Fatalf("re-normalization diverged at %d: %q vs %q", i, renorm[i].Content, persisted[i].Content)
		}
	}
}

// TestSummaryRequestReusesFrozenTools pins the tool-schema half of the cached
// unit: when frozen main-request bytes exist, the summary request must send
// the FROZEN tool schemas (byte-identical to the main request's), not the
// live registry — the server caches system+tools+messages as one prefix and
// the live tool set drifts (MCP registration) after the main request
// (2026-08-31: hit=16896 of 256122 on desktop, tools seam unaligned).
func TestSummaryRequestReusesFrozenTools(t *testing.T) {
	a := newFoldAgent(t, 200000, &countingProvider{reply: "digest"})
	frozen := []provider.ToolSchema{
		{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	mainReq := []provider.Message{{Role: provider.RoleSystem, Content: "s"}}
	a.saveMainRequest(mainReq, frozen)

	req := a.summaryRequest(mainReq, nil, "")
	if len(req.Tools) != len(frozen) || req.Tools[0].Name != "read" {
		t.Fatalf("summary tools = %+v, want the frozen schemas", req.Tools)
	}

	// Without frozen bytes the live registry supplies the schemas (empty here).
	a.sess.lastMainReq.Store(nil)
	req2 := a.summaryRequest(mainReq, nil, "")
	if len(req2.Tools) != 0 {
		t.Fatalf("without frozen bytes tools = %+v, want live registry (empty)", req2.Tools)
	}

	// Legacy sidecar: frozen messages restored but no frozen tools. The live
	// registry must supply the schemas — an empty tool list would diverge
	// from the real main-request prefix at the tools seam.
	a.saveMainRequest(mainReq, nil)
	req3 := a.summaryRequest(mainReq, nil, "")
	if len(req3.Tools) != 0 {
		t.Fatalf("frozen-messages-only tools = %+v, want live registry (empty)", req3.Tools)
	}
	commitTools := a.summaryRequestToolsForCommit(mainReq)
	if len(commitTools) != 0 {
		t.Fatalf("commit tools = %+v, want live registry (empty)", commitTools)
	}
}
