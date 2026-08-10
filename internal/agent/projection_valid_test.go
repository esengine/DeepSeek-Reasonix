package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestProjectionValidRejectsEditedPrefix(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task-v1"},
		{Role: provider.RoleAssistant, Content: "done"},
		{Role: provider.RoleUser, Content: "next"},
	}
	st := CompactionState{
		TranscriptVersion: 2,
		PromptCacheKey:    "ws|sess|model",
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "summary"},
			},
			TranscriptVersion: 2,
			CoveredCount:      3,
			CoveredPrefixHash: coveredPrefixHash(msgs, 3),
		},
	}
	if !projectionValid(st, msgs, 2, "ws|sess|model") {
		t.Fatal("expected valid projection for matching prefix")
	}
	// Append-only growth still valid.
	grown := append(append([]provider.Message(nil), msgs...), provider.Message{Role: provider.RoleAssistant, Content: "more"})
	if !projectionValid(st, grown, 3, "ws|sess|model") {
		t.Fatal("append-only growth should keep projection valid")
	}
	// Prefix edit invalidates.
	edited := append([]provider.Message(nil), msgs...)
	edited[1].Content = "task-EDITED"
	if projectionValid(st, edited, 4, "ws|sess|model") {
		t.Fatal("edited covered prefix must invalidate projection")
	}
}

func TestProjectionValidRejectsCacheKeyMismatch(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}
	hash := coveredPrefixHash(msgs, 2)
	st := CompactionState{
		TranscriptVersion: 1,
		PromptCacheKey:    "ws|sess|model-a",
		Projection: ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "sys"}},
			CoveredCount:      2,
			CoveredPrefixHash: hash,
			TranscriptVersion: 1,
		},
	}
	if projectionValid(st, msgs, 1, "ws|sess|model-b") {
		t.Fatal("model/lineage key mismatch must invalidate projection")
	}
	if !projectionValid(st, msgs, 1, "ws|sess|model-a") {
		t.Fatal("matching key should be valid")
	}
	// Fail closed: blank stored key is rejected when current key is known.
	st.PromptCacheKey = ""
	if projectionValid(st, msgs, 1, "ws|sess|model-a") {
		t.Fatal("missing sidecar cache key must invalidate when lineage is known")
	}
	// Missing prefix hash is always rejected.
	st.PromptCacheKey = "ws|sess|model-a"
	st.Projection.CoveredPrefixHash = ""
	if projectionValid(st, msgs, 1, "ws|sess|model-a") {
		t.Fatal("missing CoveredPrefixHash must invalidate projection")
	}
}

func TestCoveredPrefixHashIncludesProviderVisibleFields(t *testing.T) {
	base := []provider.Message{{
		Role:               provider.RoleAssistant,
		Content:            "answer",
		ReasoningContent:   "think",
		ReasoningID:        "rid-1",
		ReasoningStatus:    "completed",
		ReasoningSignature: "sig-1",
		Images:             []string{"data:image/png;base64,AAA"},
		ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "f", Arguments: `{}`, ThoughtSignature: "ts-1",
		}},
		ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"web_search_call"}`)},
	}}
	h1 := coveredPrefixHash(base, 1)
	if h1 == "" {
		t.Fatal("empty fingerprint")
	}
	// Each provider-visible field change must move the hash.
	cases := []struct {
		name string
		mut  func([]provider.Message)
	}{
		{"images", func(m []provider.Message) { m[0].Images = []string{"data:image/png;base64,BBB"} }},
		{"reasoning_id", func(m []provider.Message) { m[0].ReasoningID = "rid-2" }},
		{"reasoning_status", func(m []provider.Message) { m[0].ReasoningStatus = "in_progress" }},
		{"reasoning_signature", func(m []provider.Message) { m[0].ReasoningSignature = "sig-2" }},
		{"thought_signature", func(m []provider.Message) { m[0].ToolCalls[0].ThoughtSignature = "ts-2" }},
		{"responses_items", func(m []provider.Message) {
			m[0].ResponsesItems = []json.RawMessage{json.RawMessage(`{"type":"other"}`)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := []provider.Message{base[0]}
			mutated[0].ToolCalls = append([]provider.ToolCall(nil), base[0].ToolCalls...)
			mutated[0].Images = append([]string(nil), base[0].Images...)
			mutated[0].ResponsesItems = append([]json.RawMessage(nil), base[0].ResponsesItems...)
			tc.mut(mutated)
			if coveredPrefixHash(mutated, 1) == h1 {
				t.Fatalf("%s change did not alter coveredPrefixHash", tc.name)
			}
		})
	}
}

func TestLoadProjectionSidecarDropsForeignCacheKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	if err := SaveCompactionState(path, CompactionState{
		SchemaVersion:  compactionStateSchemaV1,
		PromptCacheKey: "ws|s|other-model",
		Projection: ContextProjection{
			Messages:          msgs,
			CoveredCount:      1,
			CoveredPrefixHash: coveredPrefixHash(msgs, 1),
		},
	}); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil, NewSession("sys"), Options{
		SessionPath: path,
		WorkspaceID: "ws",
		ModelRef:    "this-model",
	}, event.Discard)
	// New() already called LoadProjectionSidecar; foreign key must be dropped.
	if len(a.compactionState.Projection.Messages) != 0 {
		t.Fatalf("foreign projection loaded: %+v", a.compactionState.Projection)
	}
	// Sidecar file remains for the other model.
	if _, ok, err := LoadCompactionState(path); err != nil || !ok {
		t.Fatalf("sidecar should remain on disk: ok=%v err=%v", ok, err)
	}
}

func TestForceThresholdNoopReturnsCompactionRequired(t *testing.T) {
	// Huge tool result is entirely in the recent tail → no fold region, but
	// estimate exceeds force; preflight must refuse (not mid-turn).
	huge := strings.Repeat("word ", 5000)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read", Content: huge},
	}}
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{
		ContextWindow:     200,
		CompactRatio:      0.5,
		CompactForceRatio: 0.6,
		RecentKeep:        2,
	}, event.Discard)

	_, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure})
	if err == nil {
		t.Fatal("expected ErrCompactionRequired when force threshold has no fold region")
	}
	if !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("err = %v, want ErrCompactionRequired", err)
	}
}

func TestSummarizeWithRetryMergesUsage(t *testing.T) {
	fp := &retryUsageProvider{
		failOnce: errors.New("transient"),
		reply:    "digest body",
		usage1:   &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, RequestCount: 1},
		usage2:   &provider.Usage{PromptTokens: 11, CompletionTokens: 3, TotalTokens: 14, RequestCount: 1},
	}
	a := New(fp, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	summary, usage, err := a.summarizeWithRetry(context.Background(), []provider.Message{
		{Role: provider.RoleUser, Content: "fold me"},
	}, "")
	if err != nil {
		t.Fatalf("summarizeWithRetry: %v", err)
	}
	if summary != "digest body" {
		t.Fatalf("summary = %q", summary)
	}
	if usage == nil {
		t.Fatal("usage is nil")
	}
	// Both attempts contribute billable tokens and request count.
	if usage.PromptTokens < 21 || usage.CompletionTokens < 5 {
		t.Fatalf("merged usage under-counted: %+v", usage)
	}
	if usage.RequestCount < 2 {
		t.Fatalf("RequestCount = %d, want >= 2 (both attempts)", usage.RequestCount)
	}
}

// retryUsageProvider fails the first Stream, then returns reply + usage2.
type retryUsageProvider struct {
	calls    int
	failOnce error
	reply    string
	usage1   *provider.Usage
	usage2   *provider.Usage
}

func (p *retryUsageProvider) Name() string { return "retry-usage" }
func (p *retryUsageProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 4)
	if p.calls == 1 && p.failOnce != nil {
		if p.usage1 != nil {
			ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage1}
		}
		ch <- provider.Chunk{Type: provider.ChunkError, Err: p.failOnce}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.reply}
	if p.usage2 != nil {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage2}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestCompactInstallsCoveredPrefixHash(t *testing.T) {
	fp := &fakeProvider{reply: "digest"}
	sess := NewSession("sys")
	for range 8 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 80)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 120)})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	a := New(fp, tool.NewRegistry(), sess, Options{
		ContextWindow: 2000,
		RecentKeep:    2,
		ArchiveDir:    dir,
		SessionPath:   path,
		WorkspaceID:   "ws",
		ModelRef:      "m",
	}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	st := a.compactionState
	if st.Projection.CoveredPrefixHash == "" {
		t.Fatal("CoveredPrefixHash not set")
	}
	if st.PromptCacheKey != promptCacheKey("ws", BranchID(path), "m") {
		t.Fatalf("PromptCacheKey = %q", st.PromptCacheKey)
	}
	msgs, ver := sess.snapshotMessagesVersion()
	if !projectionValid(st, msgs, ver, st.PromptCacheKey) {
		t.Fatal("fresh projection should validate")
	}
}
