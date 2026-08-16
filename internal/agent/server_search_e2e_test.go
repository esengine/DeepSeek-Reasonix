package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type strictAssistantReasoningProvider struct{ *testutil.MockProvider }

func (p strictAssistantReasoningProvider) RequiresToolCallReasoning() bool      { return true }
func (p strictAssistantReasoningProvider) WarnOnMissingToolCallReasoning() bool { return true }
func (p strictAssistantReasoningProvider) RequiresAssistantReasoningReplay(m provider.Message) bool {
	return len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0
}

type strictRoundTripReasoningProvider struct{ *testutil.MockProvider }

func (p strictRoundTripReasoningProvider) RequiresReasoningRoundTrip() bool { return true }

func TestRunPersistsServerSearchAndEmitsCardEvents(t *testing.T) {
	search := provider.ServerSearchCall{
		ID:      "s1",
		Query:   "latest",
		Results: []provider.ServerSearchHit{{Title: "Change Log", URL: "https://api-docs.deepseek.com/updates/"}},
		Raw:     json.RawMessage(`[{"title":"Change Log","encrypted_content":"xxx"}]`),
	}
	sink := &recordSink{}
	prov := testutil.NewMock("deepseek", testutil.Turn{Chunks: []provider.Chunk{
		{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: search.ID}},
		{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: search.ID, Query: search.Query}},
		{Type: provider.ChunkServerSearch, ServerSearch: &search},
		{Type: provider.ChunkText, Text: "answer only"},
		{Type: provider.ChunkDone},
	}})
	session := NewSession("system")
	agent := New(prov, tool.NewRegistry(), session, Options{}, sink)
	if err := agent.Run(context.Background(), "search"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assistant := session.Snapshot()[len(session.Snapshot())-1]
	if assistant.Content != "answer only" || len(assistant.ServerSearch) != 1 || assistant.ServerSearch[0].ID != "s1" || assistant.ServerSearch[0].Query != "latest" {
		t.Fatalf("persisted assistant = %#v", assistant)
	}
	if string(assistant.ServerSearch[0].Raw) != string(search.Raw) {
		t.Fatalf("persisted raw = %s", assistant.ServerSearch[0].Raw)
	}

	dispatches := sink.kinds(event.ToolDispatch)
	results := sink.kinds(event.ToolResult)
	if len(dispatches) == 0 || dispatches[0].Tool.Name != "web_search" || dispatches[0].Tool.ID != "s1" {
		t.Fatalf("dispatches = %#v", dispatches)
	}
	if len(results) != 1 || results[0].Tool.Name != "web_search" || !strings.Contains(results[0].Tool.Output, "Change Log") {
		t.Fatalf("results = %#v", results)
	}

	path := filepath.Join(t.TempDir(), "server-search.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	loadedAssistant := loaded.Messages[len(loaded.Messages)-1]
	if len(loadedAssistant.ServerSearch) != 1 || loadedAssistant.ServerSearch[0].Query != "latest" {
		t.Fatalf("reloaded ServerSearch = %#v", loadedAssistant.ServerSearch)
	}
}

func serverSearchChunks(search provider.ServerSearchCall, reasoning, text string) []provider.Chunk {
	var chunks []provider.Chunk
	if reasoning != "" {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkReasoning, Text: reasoning})
	}
	chunks = append(chunks,
		provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: search.ID, Query: search.Query}},
		provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: &search},
	)
	if text != "" {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: text})
	}
	return append(chunks, provider.Chunk{Type: provider.ChunkDone})
}

func TestServerSearchPreservesRawReasoningAcrossPostLLMHook(t *testing.T) {
	search := provider.ServerSearchCall{ID: "s1", Query: "latest", Raw: json.RawMessage(`[]`)}
	mp := testutil.NewMock("deepseek", testutil.Turn{Chunks: serverSearchChunks(search, "provider original", "answer")})
	hooks := &stubHooks{hasPostLLM: true, postLLMOut: "translated display"}
	session := NewSession("system")
	a := New(strictAssistantReasoningProvider{mp}, tool.NewRegistry(), session, Options{Hooks: hooks}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "search"); err != nil {
		t.Fatal(err)
	}
	assistant := session.Snapshot()[len(session.Snapshot())-1]
	if assistant.ReasoningContent != "provider original" {
		t.Fatalf("stored reasoning = %q", assistant.ReasoningContent)
	}
}

func TestMissingServerSearchReasoningRetriesThenSalvagesHistory(t *testing.T) {
	search := provider.ServerSearchCall{ID: "s1", Query: "latest", Raw: json.RawMessage(`[]`)}
	mp := testutil.NewMock("deepseek",
		testutil.Turn{Chunks: serverSearchChunks(search, "", "answer")},
		testutil.Turn{Chunks: serverSearchChunks(search, "", "answer")},
		testutil.Turn{Text: "continued"},
	)
	sink := &recordSink{}
	session := NewSession("system")
	a := New(strictAssistantReasoningProvider{mp}, tool.NewRegistry(), session, Options{}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "search"); err != nil {
		t.Fatal(err)
	}
	assistant := session.Snapshot()[len(session.Snapshot())-1]
	if assistant.Content != "answer" || assistant.ReasoningContent != "" || len(assistant.ServerSearch) != 1 {
		t.Fatalf("salvaged assistant = %+v", assistant)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryServerSearchSalvaged); got != 1 {
		t.Fatalf("salvage audits = %d", got)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "continue"); err != nil {
		t.Fatal(err)
	}
	req := mp.Requests()[2]
	for _, m := range req.Messages {
		if len(m.ServerSearch) > 0 {
			t.Fatalf("unreplayable search leaked to next request: %+v", req.Messages)
		}
	}
	lastUser := req.Messages[len(req.Messages)-1]
	if !strings.Contains(lastUser.Content, "<interrupted-turn-recovery>") {
		t.Fatalf("missing one-shot recovery handoff: %s", lastUser.Content)
	}
}

func TestMissingClientToolReasoningFailsBeforeExecution(t *testing.T) {
	call := provider.ToolCall{ID: "c1", Name: "echo", Arguments: `{"text":"must not run"}`}
	mp := testutil.NewMock("deepseek",
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
	)
	session := NewSession("system")
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), session, Options{}, event.Discard)
	err := a.Run(withNoClosedLoop(context.Background()), "go")
	var replayErr *ReasoningReplayError
	if !errors.As(err, &replayErr) || replayErr.Kind != ReasoningReplayMissing {
		t.Fatalf("Run error = %v", err)
	}
	for _, m := range provider.ModelMessages(session.Snapshot()) {
		if len(m.ToolCalls) > 0 || m.Role == provider.RoleTool {
			t.Fatalf("unreplayable client tool committed: %+v", session.Snapshot())
		}
	}
}

func TestReasoningOverflowIsSafeForReplayContracts(t *testing.T) {
	long := strings.Repeat("思考", 32)
	call := provider.ToolCall{ID: "c1", Name: "echo", Arguments: `{"text":"no"}`}
	t.Run("client tool fails", func(t *testing.T) {
		mp := testutil.NewMock("deepseek", testutil.Turn{Reasoning: long, ToolCalls: []provider.ToolCall{call}})
		a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), NewSession("system"), Options{ReasoningByteLimit: 16}, event.Discard)
		var replayErr *ReasoningReplayError
		if err := a.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) || replayErr.Kind != ReasoningReplayOverflow {
			t.Fatalf("Run error = %v", err)
		}
	})
	t.Run("server search keeps answer", func(t *testing.T) {
		search := provider.ServerSearchCall{ID: "s1", Query: "latest", Raw: json.RawMessage(`[]`)}
		mp := testutil.NewMock("deepseek", testutil.Turn{Chunks: serverSearchChunks(search, long, "answer")})
		session := NewSession("system")
		a := New(strictAssistantReasoningProvider{mp}, tool.NewRegistry(), session, Options{ReasoningByteLimit: 16}, event.Discard)
		if err := a.Run(withNoClosedLoop(context.Background()), "search"); err != nil {
			t.Fatal(err)
		}
		assistant := session.Snapshot()[len(session.Snapshot())-1]
		if assistant.Content != "answer" || assistant.ReasoningContent != "" || len(assistant.ServerSearch) != 1 {
			t.Fatalf("assistant = %+v", assistant)
		}
	})
	t.Run("all-turn round trip fails", func(t *testing.T) {
		mp := testutil.NewMock("roundtrip", testutil.Turn{Reasoning: long, Text: "answer"})
		a := New(strictRoundTripReasoningProvider{mp}, tool.NewRegistry(), NewSession("system"), Options{ReasoningByteLimit: 16}, event.Discard)
		var replayErr *ReasoningReplayError
		if err := a.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) || replayErr.Kind != ReasoningReplayOverflow {
			t.Fatalf("Run error = %v", err)
		}
	})
}

func TestLegacyMissingReasoningToolHistoryIsProjectedOutOnce(t *testing.T) {
	call := provider.ToolCall{ID: "old", Name: "write_file", Arguments: `{"path":"done.txt","content":"done"}`}
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "write it"})
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "old", Name: "write_file", Content: "ok"})
	path := filepath.Join(t.TempDir(), "v1.25.1-poisoned.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	mp := testutil.NewMock("deepseek", testutil.Turn{Text: "continued"}, testutil.Turn{Text: "continued again"})
	a := New(strictAssistantReasoningProvider{mp}, tool.NewRegistry(), loaded, Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "continue"); err != nil {
		t.Fatal(err)
	}
	first := mp.Requests()[0]
	for _, m := range first.Messages {
		if len(m.ToolCalls) > 0 || (m.Role == provider.RoleTool && !m.LocalOnly) {
			t.Fatalf("legacy poisoned pair leaked: %+v", first.Messages)
		}
	}
	if !strings.Contains(first.Messages[len(first.Messages)-1].Content, "<interrupted-turn-recovery>") {
		t.Fatalf("first request missing recovery: %+v", first.Messages)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err != nil {
		t.Fatal(err)
	}
	second := mp.Requests()[1]
	if strings.Contains(second.Messages[len(second.Messages)-1].Content, "<interrupted-turn-recovery>") {
		t.Fatalf("recovery repeated: %+v", second.Messages)
	}
	canonical := loaded.Snapshot()
	if len(canonical) < 3 || len(canonical[2].ToolCalls) != 1 || canonical[3].Role != provider.RoleTool {
		t.Fatalf("canonical legacy history was destroyed: %+v", canonical)
	}
}

func TestReasoningHistoryRepairKeepsHealthyAndEmptyFallbackBytes(t *testing.T) {
	healthy := []provider.Message{{
		Role: provider.RoleAssistant, ReasoningContent: "original",
		ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}},
	}}
	strict := strictAssistantReasoningProvider{testutil.NewMock("strict")}
	if got, changed := repairUnreplayableReasoningHistory(strict, healthy); changed || &got[0] != &healthy[0] {
		t.Fatal("healthy replay history must keep its exact backing slice")
	}

	emptyFallback := []provider.Message{{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}},
	}}
	openAIStyle := toolCallReasoningRequiredProvider{testutil.NewMock("openai")}
	if got, changed := repairUnreplayableReasoningHistory(openAIStyle, emptyFallback); changed || &got[0] != &emptyFallback[0] {
		t.Fatal("empty-key fallback history must remain byte-identical")
	}
}
