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

func TestSplitExtractChunksCountsReplayResponsesItems(t *testing.T) {
	largeItem := json.RawMessage(`"` + strings.Repeat("r", extractChunkNewestBytes) + `"`)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "old"},
		{Role: provider.RoleAssistant, Content: "middle", ResponsesItems: []json.RawMessage{largeItem}},
		{Role: provider.RoleUser, Content: "new"},
	}

	replayPolicy := provider.SharedWindowInputPolicy{ReplaysResponsesItems: true}
	if chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, replayPolicy); len(chunks) < 2 {
		t.Fatalf("replay-aware chunks = %d, want at least 2", len(chunks))
	}
	if chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, provider.SharedWindowInputPolicy{}); len(chunks) != 1 {
		t.Fatalf("non-replay chunks = %d, want 1", len(chunks))
	}
}

func TestMessageWireBytesCountsProviderVisibleReplayFields(t *testing.T) {
	msg := provider.Message{
		Role:               provider.RoleAssistant,
		Content:            "content",
		Images:             []string{"image-ref"},
		ReasoningContent:   "reasoning",
		ReasoningID:        "reasoning-id",
		ReasoningStatus:    "completed",
		ReasoningSignature: "reasoning-signature",
		ToolCalls: []provider.ToolCall{{
			ID:               "call-id",
			Name:             "tool-name",
			Arguments:        `{"key":"value"}`,
			ThoughtSignature: "thought-signature",
		}},
		ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning"}`)},
		ServerSearch: []provider.ServerSearchCall{{
			ID:    "search-id",
			Query: "search-query",
			Results: []provider.ServerSearchHit{{
				Title: "result-title",
				URL:   "https://example.test/result",
			}},
			Raw: json.RawMessage(`{"encrypted_content":"must-not-count"}`),
		}},
	}
	policy := provider.SharedWindowInputPolicy{ReplaysResponsesItems: true}
	want := 4 + len(string(msg.Role)) + len(msg.Content) + len(msg.ReasoningContent) +
		8 + len(msg.ToolCalls[0].ID) + len(msg.ToolCalls[0].Name) + len(msg.ToolCalls[0].Arguments) +
		len(msg.ResponsesItems[0]) + len(msg.ServerSearch[0].ID) + len(msg.ServerSearch[0].Query) +
		len(msg.ServerSearch[0].Results[0].Title) + len(msg.ServerSearch[0].Results[0].URL) +
		len(msg.ReasoningID) + len(msg.ReasoningStatus) + len(msg.ReasoningSignature) +
		len(msg.ToolCalls[0].ThoughtSignature) + len(msg.Images[0])
	if got := messageWireBytes(msg, policy); got != want {
		t.Fatalf("wire bytes = %d, want %d", got, want)
	}

	withoutRaw := msg
	withoutRaw.ServerSearch = append([]provider.ServerSearchCall(nil), msg.ServerSearch...)
	withoutRaw.ServerSearch[0].Raw = nil
	if got, want := messageWireBytes(msg, policy), messageWireBytes(withoutRaw, policy); got != want {
		t.Fatalf("ServerSearch.Raw changed wire estimate: with=%d without=%d", got, want)
	}

	withoutItems := messageWireBytes(msg, provider.SharedWindowInputPolicy{})
	if got := messageWireBytes(msg, policy) - withoutItems; got != len(msg.ResponsesItems[0]) {
		t.Fatalf("ResponsesItems replay bytes = %d, want %d", got, len(msg.ResponsesItems[0]))
	}
}

func TestSummarizeExtractChunksPreflightsMinimumPlan(t *testing.T) {
	chunk := []provider.Message{{Role: provider.RoleUser, Content: "fragment"}}
	tests := []struct {
		name      string
		count     int
		wantCalls int
		wantErr   bool
	}{
		{name: "63 chunks fit 64 calls", count: 63, wantCalls: 64},
		{name: "64 chunks require 65 calls", count: 64, wantCalls: 0, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prov := &extractStubProvider{reply: "digest"}
			a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
			chunks := make([][]provider.Message, tc.count)
			for i := range chunks {
				chunks[i] = chunk
			}
			_, err := a.summarizeExtractChunks(context.Background(), chunks, "", nil, newChunkedSummaryRun(a))
			if (err != nil) != tc.wantErr {
				t.Fatalf("summarizeExtractChunks error = %v, wantErr %v", err, tc.wantErr)
			}
			if prov.calls != tc.wantCalls {
				t.Fatalf("provider calls = %d, want %d", prov.calls, tc.wantCalls)
			}
		})
	}
}

func TestChunkedSummaryRunReservesCallsAfterRequest(t *testing.T) {
	prov := &extractStubProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	run := newChunkedSummaryRun(a)
	run.calls = maxChunkedSummaryCalls - 1
	_, err := run.summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "x"}}, extractMergeInstruction, 1)
	if err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
		t.Fatalf("reservation error = %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", prov.calls)
	}
}

func TestRecursiveRecoveryPreservesOuterCallBudget(t *testing.T) {
	t.Run("fragment split", func(t *testing.T) {
		prov := &extractStubProvider{failFirst: 1, reply: "digest"}
		a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
		run := newChunkedSummaryRun(a)
		run.calls = maxChunkedSummaryCalls - 4
		chunk := []provider.Message{
			{Role: provider.RoleUser, Content: "left"},
			{Role: provider.RoleUser, Content: "right"},
		}
		_, err := a.extractFragmentResilient(context.Background(), chunk, extractFragmentInstruction(1, 1, ""), extractMergeInstruction, func(bool) {}, run, 1)
		if err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
			t.Fatalf("fragment recovery error = %v", err)
		}
		if prov.calls != 1 {
			t.Fatalf("provider calls = %d, want 1 failed root and no doomed split", prov.calls)
		}
	})

	t.Run("merge split", func(t *testing.T) {
		prov := &extractStubProvider{failFirst: 1, reply: "digest"}
		a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
		run := newChunkedSummaryRun(a)
		run.calls = maxChunkedSummaryCalls - 4
		_, err := a.mergeGroup(context.Background(), []string{"left", "right"}, extractMergeInstruction, run, 0, 1)
		if err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
			t.Fatalf("merge recovery error = %v", err)
		}
		if prov.calls != 1 {
			t.Fatalf("provider calls = %d, want 1 failed root and no doomed split", prov.calls)
		}
	})
}

func TestMergeTreeRechecksBudgetBeforeNewRound(t *testing.T) {
	prov := &extractStubProvider{reply: strings.Repeat("digest ", 320)}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{ContextWindow: 2000}, event.Discard)
	run := newChunkedSummaryRun(a)
	run.calls = maxChunkedSummaryCalls - 3
	parts := []string{
		strings.Repeat("one ", 320),
		strings.Repeat("two ", 320),
		strings.Repeat("three ", 320),
		strings.Repeat("four ", 320),
		strings.Repeat("five ", 320),
	}
	_, err := a.mergeFragmentsWithRun(context.Background(), parts, extractMergeInstruction, run, 0)
	if err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
		t.Fatalf("merge tree error = %v", err)
	}
	if prov.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 first-round pairs and no doomed second round", prov.calls)
	}
}
