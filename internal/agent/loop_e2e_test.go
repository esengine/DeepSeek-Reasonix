package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type toolCallReasoningRequiredProvider struct {
	*testutil.MockProvider
}

func (p toolCallReasoningRequiredProvider) RequiresToolCallReasoning() bool { return true }

type configuredToolCallReasoningProvider struct {
	*testutil.MockProvider
	identity string
}

type cancelMissingReasoningRetryProvider struct {
	calls          atomic.Int32
	retryUsageSent chan struct{}
}

func (p *cancelMissingReasoningRetryProvider) Name() string { return "deepseek-cancel-retry" }
func (p *cancelMissingReasoningRetryProvider) RequiresToolCallReasoning() bool {
	return true
}

func (p *cancelMissingReasoningRetryProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	call := p.calls.Add(1)
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		send := func(chunk provider.Chunk) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- chunk:
				return true
			}
		}
		if call == 1 {
			toolCall := provider.ToolCall{ID: "discarded", Name: "echo", Arguments: `{"text":"must not run"}`}
			if !send(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &toolCall}) {
				return
			}
			if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}}) {
				return
			}
			send(provider.Chunk{Type: provider.ChunkDone})
			return
		}
		if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}}) {
			return
		}
		close(p.retryUsageSent)
		<-ctx.Done()
	}()
	return ch, nil
}

func (p configuredToolCallReasoningProvider) RequiresToolCallReasoning() bool { return true }
func (p configuredToolCallReasoningProvider) MissingToolCallReasoningWarningIdentity() string {
	return p.identity
}

func echoRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(echoTool{})
	return reg
}

func TestRunPersistsUserCreatedAtWithoutSendingItToProvider(t *testing.T) {
	const existingCreatedAt int64 = 1_718_000_000_000
	prov := testutil.NewMock("m", testutil.Turn{Text: "done"})
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "existing", CreatedAt: existingCreatedAt})
	agent := New(prov, tool.NewRegistry(), session, Options{}, event.Discard)

	if err := agent.Run(context.Background(), "new prompt"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	request := prov.LastRequest()
	if request == nil {
		t.Fatal("provider received no request")
	}
	for i, message := range request.Messages {
		if message.CreatedAt != 0 {
			t.Fatalf("provider message %d leaked createdAt %d", i, message.CreatedAt)
		}
	}

	messages := session.Snapshot()
	if len(messages) < 3 || messages[1].CreatedAt != existingCreatedAt {
		t.Fatalf("persisted existing timestamp changed: %+v", messages)
	}
	if messages[2].Role != provider.RoleUser || messages[2].CreatedAt <= 0 {
		t.Fatalf("new user timestamp was not persisted: %+v", messages[2])
	}
}

func TestRunPersistsResponsesItemsAcrossSessionReload(t *testing.T) {
	raw := json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"latest"}}`)
	prov := testutil.NewMock("deepseek-responses", testutil.Turn{Chunks: []provider.Chunk{
		{Type: provider.ChunkResponsesItem, ResponsesItem: raw},
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}})
	session := NewSession("system")
	agent := New(prov, tool.NewRegistry(), session, Options{}, event.Discard)
	if err := agent.Run(context.Background(), "search"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	messages := session.Snapshot()
	assistant := messages[len(messages)-1]
	if assistant.Role != provider.RoleAssistant || len(assistant.ResponsesItems) != 1 || string(assistant.ResponsesItems[0]) != string(raw) {
		t.Fatalf("assistant Responses items = %#v, want persisted search item", assistant.ResponsesItems)
	}

	path := filepath.Join(t.TempDir(), "responses-items.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	loadedAssistant := loaded.Messages[len(loaded.Messages)-1]
	if len(loadedAssistant.ResponsesItems) != 1 || string(loadedAssistant.ResponsesItems[0]) != string(raw) {
		t.Fatalf("reloaded Responses items = %#v, want original item", loadedAssistant.ResponsesItems)
	}
}

// TestRunMultiToolRoundEmptyIDsSurvivePairing drives the real loop through a turn
// that fans out two tool calls carrying no id (a gateway that streams by index),
// then asserts both results still pair back after SanitizeToolPairing — the repair
// that runs on every send. Keying on tool_call_id alone collapsed them into one,
// dropping a result from the model's context on the very next turn.
func TestRunMultiToolRoundEmptyIDsSurvivePairing(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "", Name: "echo", Arguments: `{"text":"alpha"}`},
			{ID: "", Name: "echo", Arguments: `{"text":"beta"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	var results []string
	for _, m := range repaired {
		if m.Role == provider.RoleTool {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 {
		t.Fatalf("want 2 tool results after pairing, got %d: %v", len(results), results)
	}
	if results[0] == results[1] {
		t.Fatalf("both results collapsed to %q — one was lost from the model's context", results[0])
	}
	if !strings.Contains(results[0], "alpha") || !strings.Contains(results[1], "beta") {
		t.Errorf("results lost their identity: %v", results)
	}
}

func TestRunPersistsCumulativeAssistantWorkDuration(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "echo", Arguments: `{"text":"hello"}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var durations []int64
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleAssistant {
			durations = append(durations, message.WorkDurationMs)
		}
	}
	if len(durations) != 2 {
		t.Fatalf("assistant durations = %v, want two rounds", durations)
	}
	if durations[0] <= 0 || durations[1] < durations[0] {
		t.Fatalf("assistant durations must be positive and cumulative: %v", durations)
	}
}

// TestRunCancelledMidStreamLeavesResumableSession proves a turn cancelled before
// the model answered leaves the session well-formed: the user message stands,
// nothing dangling, and the repaired history is sendable as-is on resume.
func TestRunCancelledMidStreamLeavesResumableSession(t *testing.T) {
	mp := testutil.NewMock("m", testutil.ErrorTurn(context.Canceled))
	a := New(mp, echoRegistry(), NewSession("sys"), Options{}, event.Discard)

	err := a.Run(context.Background(), "do the thing")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run should surface the cancellation, got %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	for i, m := range repaired {
		if m.Role == provider.RoleTool {
			t.Fatalf("a cancelled turn left a dangling tool message at %d: %+v", i, m)
		}
	}
	last := repaired[len(repaired)-1]
	if last.Role != provider.RoleUser || last.Content != "do the thing" {
		t.Errorf("the pending user message should survive a cancel, got %+v", last)
	}
}

func TestRunRecoversInterruptedStreamAfterPartialText(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "partial ", ChunkError: interrupted},
		testutil.Turn{Text: "continued"},
	)
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover the interrupted stream, got %v", err)
	}
	if mp.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want 2", mp.CallCount())
	}

	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded requests = %d, want 2", len(reqs))
	}
	second := reqs[1].Messages
	for _, message := range second {
		if message.LocalOnly || message.Content == "partial " {
			t.Fatalf("partial assistant leaked into provider recovery request: %+v", second)
		}
	}
	if second[len(second)-1].Role != provider.RoleUser || !strings.Contains(second[len(second)-1].Content, "excluded from model context") {
		t.Fatalf("recovery prompt missing duplicate guard: %+v", second[len(second)-1])
	}
	var local provider.Message
	for _, message := range a.Session().Messages {
		if message.LocalOnly {
			local = message
		}
	}
	if local.Content != "partial " || local.InterruptedTurn == nil || local.InterruptedTurn.Pending {
		t.Fatalf("partial assistant was not retained as consumed display-only history: %+v", local)
	}

	var streamed strings.Builder
	for _, e := range sink.kinds(event.Text) {
		streamed.WriteString(e.Text)
	}
	if streamed.String() != "partial continued" {
		t.Fatalf("streamed text = %q, want %q", streamed.String(), "partial continued")
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 1 || retries[0].RetryAttempt != 1 || retries[0].RetryMax != maxStreamRecoveries {
		t.Fatalf("retry events = %+v, want one stream recovery retry", retries)
	}
}

func TestRunRecoversRepeatedInterruptedStreams(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "first ", ChunkError: interrupted},
		testutil.Turn{Text: "second ", ChunkError: interrupted},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover repeated interrupted streams, got %v", err)
	}
	if mp.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want 3", mp.CallCount())
	}

	var streamed strings.Builder
	for _, e := range sink.kinds(event.Text) {
		streamed.WriteString(e.Text)
	}
	if streamed.String() != "first second done" {
		t.Fatalf("streamed text = %q, want repeated partials plus final text", streamed.String())
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 2 || retries[0].RetryAttempt != 1 || retries[1].RetryAttempt != 2 {
		t.Fatalf("retry events = %+v, want attempts 1 and 2", retries)
	}
	for _, retry := range retries {
		if retry.RetryMax != maxStreamRecoveries {
			t.Fatalf("retry max = %d, want %d", retry.RetryMax, maxStreamRecoveries)
		}
	}
}

func TestRunRecoversInterruptedPartialToolCallWithoutExecutingIt(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "c1", Name: "echo"}},
			{Type: provider.ChunkError, Err: interrupted},
		}},
		testutil.Turn{Text: "recovered"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover the interrupted tool-call stream, got %v", err)
	}

	var displayOnly provider.Message
	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleTool && !m.LocalOnly {
			t.Fatalf("partial tool call should not have executed or produced a tool result: %+v", m)
		}
		if m.LocalOnly {
			displayOnly = m
		}
	}
	if len(displayOnly.ToolCalls) != 1 || displayOnly.ToolCalls[0].Name != "echo" || displayOnly.ToolCalls[0].Arguments != "" {
		t.Fatalf("partial tool call was not retained safely for display: %+v", displayOnly)
	}
	reqs := mp.Requests()
	second := reqs[1].Messages
	last := second[len(second)-1]
	if last.Role != provider.RoleUser || !strings.Contains(last.Content, "fresh complete tool call") {
		t.Fatalf("partial-tool recovery prompt missing fresh-call instruction: %+v", last)
	}
}

func TestRunGenericStreamErrorPersistsLocalDisplayAndInjectsBoundedRecovery(t *testing.T) {
	apiErr := errors.New("upstream reset")
	mp := testutil.NewMock("m",
		testutil.Turn{Reasoning: "private partial reasoning", Text: "visible partial", ChunkError: apiErr},
		testutil.Turn{Text: "continued safely"},
	)
	session := NewSession("system")
	a := New(mp, echoRegistry(), session, Options{}, event.Discard)

	if err := a.Run(context.Background(), "change the file"); !errors.Is(err, apiErr) {
		t.Fatalf("first Run error = %v, want %v", err, apiErr)
	}
	msgs := session.Snapshot()
	last := msgs[len(msgs)-1]
	if !last.LocalOnly || last.InterruptedTurn == nil || !last.InterruptedTurn.Pending {
		t.Fatalf("terminal stream error did not leave pending local recovery: %+v", last)
	}
	if last.Content != "visible partial" || last.ReasoningContent != "private partial reasoning" {
		t.Fatalf("local display lost streamed output: %+v", last)
	}

	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	req := mp.Requests()[1]
	for _, message := range req.Messages {
		if message.LocalOnly || strings.Contains(message.Content, "visible partial") || strings.Contains(message.ReasoningContent, "private partial reasoning") {
			t.Fatalf("unsafe partial output leaked to provider: %+v", req.Messages)
		}
	}
	lastUser := req.Messages[len(req.Messages)-1]
	if lastUser.Role != provider.RoleUser || !strings.Contains(lastUser.Content, "<interrupted-turn-recovery>") ||
		!strings.Contains(lastUser.Content, "unsafe_partial_output: excluded") || !strings.HasSuffix(lastUser.Content, "continue") {
		t.Fatalf("next user turn missing bounded recovery block: %+v", lastUser)
	}
	if got := StripTransientUserBlocks(lastUser.Content); got != "continue" {
		t.Fatalf("recovery block leaked into user display: %q", got)
	}
}

func TestRunRecoveryKeepsCompletedToolPairAndSummarizesChangedFile(t *testing.T) {
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "update config"})
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "done-1", Name: "write_file", Arguments: `{"path":"config.json","content":"{}"}`, Added: 1,
	}}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "done-1", Name: "write_file", Content: "wrote config.json"})
	session.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true,
		ReasoningContent: "unsafe partial reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{
			Pending: true,
			CompletedTools: []provider.InterruptedToolSummary{{
				ID: "done-1", Name: "write_file", Files: []string{"config.json"}, Added: 1,
			}},
			InterruptedTools:        []string{"bash"},
			DroppedPartialReasoning: true,
		},
	})
	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), session, Options{}, event.Discard)
	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	req := mp.Requests()[0]
	if len(req.Messages) != 5 {
		t.Fatalf("provider request should contain system + user + complete pair + recovery user, got %+v", req.Messages)
	}
	if req.Messages[2].Role != provider.RoleAssistant || req.Messages[3].Role != provider.RoleTool {
		t.Fatalf("completed tool pair was not replayed canonically: %+v", req.Messages)
	}
	last := req.Messages[len(req.Messages)-1]
	for _, want := range []string{"write_file files=config.json diff=+1/-0", "interrupted_tools: bash", "inspect the current workspace", "continue"} {
		if !strings.Contains(last.Content, want) {
			t.Fatalf("recovery user message missing %q: %s", want, last.Content)
		}
	}
	if strings.Contains(last.Content, "unsafe partial reasoning") {
		t.Fatalf("raw partial reasoning leaked into recovery summary: %s", last.Content)
	}
}

// TestRunWellFormedToolLoopRoundTrips is the happy-path baseline: a tool round
// then a final answer. The session must end with the assistant answer and pair
// cleanly (the repair is a no-op on well-formed histories).
func TestRunWellFormedToolLoopRoundTrips(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "all set"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := a.Session().Messages
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.Content != "all set" {
		t.Fatalf("final message should be the assistant answer, got %+v", last)
	}
	before := len(msgs)
	if after := len(provider.SanitizeToolPairing(msgs)); after != before {
		t.Errorf("repair mutated a well-formed session: %d -> %d", before, after)
	}
}

// A provider without the DeepSeek tool-call reasoning policy must keep the
// ordinary two-call tool loop even when its tool-call turn has no reasoning.
func TestRunNonDeepSeekMissingToolCallReasoningDoesNotRetry(t *testing.T) {
	mp := testutil.NewMock("openai",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "all set"},
	)
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := mp.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want tool turn + final turn without recovery retry", got)
	}
	if got := len(sink.kinds(event.ToolDispatch)); got != 1 {
		t.Fatalf("tool dispatches = %d, want one", got)
	}
	sink.mu.Lock()
	recovery := append([]event.ProtocolRecoveryAudit(nil), sink.recovery...)
	sink.mu.Unlock()
	if len(recovery) != 0 {
		t.Fatalf("non-DeepSeek provider emitted protocol recovery audits: %+v", recovery)
	}
}

// A one-off missing reasoning_content response is replaced before any tool
// executes. The retry reuses identical input, its usage is accounted for, and
// no provider-protocol warning or duplicate tool card reaches the user.
func TestRunSilentlyRecoversMissingToolCallReasoning(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}},
			Usage:     &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, CacheMissTokens: 10, FinishReason: "tool_calls"},
		},
		testutil.Turn{
			Reasoning: "retry reasoning",
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}},
			Usage:     &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13, CacheHitTokens: 10, ReasoningTokens: 2, FinishReason: "tool_calls"},
		},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var savedToolTurns int
	var savedReasoning string
	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			savedToolTurns++
			savedReasoning = m.ReasoningContent
		}
	}
	if savedToolTurns != 1 || savedReasoning != "retry reasoning" {
		t.Fatalf("saved tool turns = %d reasoning = %q, want one recovered turn: %+v", savedToolTurns, savedReasoning, a.Session().Messages)
	}
	if mp.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want malformed + retry + final", mp.CallCount())
	}
	requests := mp.Requests()
	if len(requests) < 2 || !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatalf("protocol retry changed provider-visible request:\nfirst=%+v\nretry=%+v", requests[0], requests[1])
	}
	for _, e := range sink.kinds(event.Notice) {
		if strings.Contains(e.Text, "reasoning") || strings.Contains(e.Detail, "reasoning") {
			t.Fatalf("provider protocol leaked into user notice: %+v", e)
		}
	}
	if got := len(sink.kinds(event.ToolDispatch)); got != 1 {
		t.Fatalf("tool dispatches = %d, want one adopted call", got)
	}
	usageEvents := sink.kinds(event.Usage)
	if len(usageEvents) == 0 || usageEvents[0].Usage == nil || usageEvents[0].Usage.TotalTokens != 25 || usageEvents[0].Usage.CacheHitTokens != 10 || usageEvents[0].Usage.CacheMissTokens != 10 {
		t.Fatalf("recovery usage was not merged truthfully: %+v", usageEvents)
	}
	if sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted) != 1 || sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryRecovered) != 1 {
		t.Fatalf("unexpected recovery audit: %+v", sink.recovery)
	}
}

// An exact recovery replay may choose a normal final answer instead of
// repeating the original tool call. The replacement is authoritative because
// no tool has run yet: discard the speculative call, persist only the final
// response, and classify the outcome separately from recovered reasoning.
func TestMissingReasoningRecoveryAdoptsRetryWithoutToolCall(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "discarded", Name: "echo", Arguments: `{"text":"must not run"}`}},
			Usage:     &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, FinishReason: "tool_calls"},
		},
		testutil.Turn{
			Text:  "completed without a tool",
			Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13, FinishReason: "stop"},
		},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want malformed + replacement", mp.CallCount())
	}
	var toolTurns, toolResults int
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleAssistant && len(message.ToolCalls) > 0 {
			toolTurns++
		}
		if message.Role == provider.RoleTool {
			toolResults++
		}
	}
	if toolTurns != 0 || toolResults != 0 {
		t.Fatalf("discarded tool response reached session: turns=%d results=%d session=%+v", toolTurns, toolResults, a.Session().Messages)
	}
	last := a.Session().Messages[len(a.Session().Messages)-1]
	if last.Role != provider.RoleAssistant || last.Content != "completed without a tool" {
		t.Fatalf("replacement response not adopted: %+v", last)
	}
	if got := len(sink.kinds(event.ToolDispatch)); got != 0 {
		t.Fatalf("discarded tool dispatches = %d, want 0", got)
	}
	usageEvents := sink.kinds(event.Usage)
	if len(usageEvents) == 0 || usageEvents[0].Usage == nil || usageEvents[0].Usage.TotalTokens != 25 {
		t.Fatalf("replacement usage was not merged truthfully: %+v", usageEvents)
	}
	if sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted) != 1 ||
		sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryReplaced) != 1 ||
		sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryRecovered) != 0 ||
		sink.recoveryCount(event.ProtocolRecoveryMissingReasoningFallback) != 0 {
		t.Fatalf("unexpected recovery classification: %+v", sink.recovery)
	}
}

func TestMissingReasoningRecoveryFailureFallsBackBeforeToolExecution(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}},
			Usage:     &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, FinishReason: "tool_calls"},
		},
		testutil.Turn{
			Usage:      &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11},
			ChunkError: errors.New("recovery stream failed"),
		},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should keep the complete first response, got %v", err)
	}
	var toolResults int
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleTool && message.ToolCallID == "c1" {
			toolResults++
		}
	}
	if toolResults != 1 {
		t.Fatalf("tool results = %d, want the original call executed once", toolResults)
	}
	usageEvents := sink.kinds(event.Usage)
	if len(usageEvents) == 0 || usageEvents[0].Usage == nil || usageEvents[0].Usage.TotalTokens != 23 {
		t.Fatalf("failed recovery usage was not accounted for: %+v", usageEvents)
	}
	if sink.recoveryCount(event.ProtocolRecoveryMissingReasoningFallback) != 1 {
		t.Fatalf("fallback audit missing: %+v", sink.recovery)
	}
}

func TestMissingReasoningRecoveryCancellationAccountsBothAttempts(t *testing.T) {
	prov := &cancelMissingReasoningRetryProvider{retryUsageSent: make(chan struct{})}
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "go") }()

	select {
	case <-prov.retryUsageSent:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for the recovery retry usage")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
	if got := prov.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want malformed response plus recovery retry", got)
	}
	if got := len(sink.kinds(event.ToolDispatch)); got != 0 {
		t.Fatalf("discarded tool dispatches = %d, want 0", got)
	}
	usages := sink.kinds(event.Usage)
	if len(usages) != 1 || usages[0].Usage == nil || usages[0].Usage.TotalTokens != 23 || usages[0].Usage.FinishReason != "interrupted" {
		t.Fatalf("recovery cancellation usage = %+v, want one merged interrupted total of 23", usages)
	}
}

func TestSetSessionRearmsInMemoryMissingReasoningRecovery(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1r", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2r", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done again"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	a.SetSession(NewSession(""))
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 2 {
		t.Fatalf("recovery retries across two sessions = %d, want 2", got)
	}
}

// A shared state dir turns the old warning cooldown into a cross-process retry
// circuit breaker. The first process retries once; a fresh process immediately
// uses the empty-key fallback without doubling the request.
func TestMissingReasoningRecoveryRateLimitsAcrossProcesses(t *testing.T) {
	stateDir := t.TempDir()
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1r", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done"},
	)
	sink1 := &recordSink{}
	a1 := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink1)
	if err := a1.Run(context.Background(), "go"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if got := sink1.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 1 {
		t.Fatalf("first process recovery retries = %d, want 1", got)
	}

	mp2 := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done again"},
	)
	sink2 := &recordSink{}
	a2 := New(toolCallReasoningRequiredProvider{mp2}, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink2)
	if err := a2.Run(context.Background(), "go"); err != nil {
		t.Fatalf("second process Run: %v", err)
	}
	if got := sink2.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted); got != 0 {
		t.Fatalf("fresh process recovery retries = %d, want 0", got)
	}
	if got := sink2.recoveryCount(event.ProtocolRecoveryMissingReasoningRetrySuppressed); got != 1 {
		t.Fatalf("fresh process suppressed retries = %d, want 1", got)
	}
}

func TestMissingReasoningRecoverySeparatesProviderConfigurations(t *testing.T) {
	stateDir := t.TempDir()
	retryCount := func(identity string) int {
		mp := testutil.NewMock("deepseek-proxy",
			testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
			testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1r", Name: "echo", Arguments: `{"text":"hi"}`}}},
			testutil.Turn{Text: "done"},
		)
		sink := &recordSink{}
		a := New(configuredToolCallReasoningProvider{MockProvider: mp, identity: identity}, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)
		if err := a.Run(context.Background(), "go"); err != nil {
			t.Fatalf("Run(%q): %v", identity, err)
		}
		return sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted)
	}
	if got := retryCount("openai\x00endpoint-a\x00deepseek-v4-pro"); got != 1 {
		t.Fatalf("first configuration retries = %d, want 1", got)
	}
	if got := retryCount("openai\x00endpoint-a\x00deepseek-v4-pro"); got != 0 {
		t.Fatalf("same configuration retries = %d, want 0", got)
	}
	if got := retryCount("openai\x00endpoint-b\x00deepseek-v4-pro"); got != 1 {
		t.Fatalf("changed endpoint retries = %d, want 1", got)
	}
	if got := retryCount("openai\x00endpoint-a\x00deepseek-v4-flash"); got != 1 {
		t.Fatalf("changed model retries = %d, want 1", got)
	}
}

func TestThreeHealthyToolCallReasoningTurnsRearmFutureRegression(t *testing.T) {
	stateDir := t.TempDir()
	run := func(turns ...testutil.Turn) int {
		mp := testutil.NewMock("deepseek-proxy", turns...)
		sink := &recordSink{}
		a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, sink)
		if err := a.Run(context.Background(), "go"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted)
	}
	missing := testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}}
	healthy := testutil.Turn{Reasoning: "call echo", ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"hi"}`}}}
	if got := run(missing, missing, testutil.Turn{Text: "done"}); got != 1 {
		t.Fatalf("first incident retries = %d, want 1", got)
	}
	for healthyTurn := 1; healthyTurn <= missingReasoningHealthyResolveStreak; healthyTurn++ {
		if got := run(healthy, testutil.Turn{Text: "done"}); got != 0 {
			t.Fatalf("healthy turn %d retries = %d, want 0", healthyTurn, got)
		}
	}
	if got := run(missing, missing, testutil.Turn{Text: "done"}); got != 1 {
		t.Fatalf("post-recovery regression retries = %d, want 1", got)
	}
}

func TestHealthyToolCallReasoningStreakWorksWithinOneAgentAndResetsOnMissing(t *testing.T) {
	stateDir := t.TempDir()
	prov := toolCallReasoningRequiredProvider{testutil.NewMock("deepseek-proxy")}
	a := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, event.Discard)
	calls := []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}

	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || !retry {
		t.Fatalf("initial observation = missing:%v retry:%v, want true/true", missing, retry)
	}
	for healthy := 1; healthy < missingReasoningHealthyResolveStreak; healthy++ {
		a.observeMissingToolCallReasoning(calls, "healthy reasoning")
	}
	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || retry {
		t.Fatalf("missing reset = missing:%v retry:%v, want true/false", missing, retry)
	}
	for healthy := 1; healthy <= missingReasoningHealthyResolveStreak; healthy++ {
		a.observeMissingToolCallReasoning(calls, "healthy reasoning")
	}
	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || !retry {
		t.Fatalf("post-recovery observation = missing:%v retry:%v, want true/true", missing, retry)
	}
}

func TestMissingReasoningRecoveryIOFailureStillSuppressesLocally(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(statePath, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	prov := toolCallReasoningRequiredProvider{testutil.NewMock("deepseek-proxy")}
	a := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: statePath}, event.Discard)
	calls := []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}

	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || !retry {
		t.Fatalf("initial observation = missing:%v retry:%v, want true/true", missing, retry)
	}
	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || retry {
		t.Fatalf("repeated observation = missing:%v retry:%v, want true/false", missing, retry)
	}
}

func TestHealthyToolCallReasoningRetriesTransientStateWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are not portable to Windows")
	}
	stateDir := t.TempDir()
	prov := toolCallReasoningRequiredProvider{testutil.NewMock("deepseek-proxy")}
	a := New(prov, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: stateDir}, event.Discard)
	calls := []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}

	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || !retry {
		t.Fatalf("initial observation = missing:%v retry:%v, want true/true", missing, retry)
	}
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	permissionsRestored := false
	defer func() {
		if !permissionsRestored {
			_ = os.Chmod(stateDir, 0o700)
		}
	}()
	if missing, retry := a.observeMissingToolCallReasoning(calls, "healthy reasoning"); missing || retry {
		t.Fatalf("healthy observation = missing:%v retry:%v, want false/false", missing, retry)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	permissionsRestored = true
	for healthy := 0; healthy < missingReasoningHealthyResolveStreak-1; healthy++ {
		if missing, retry := a.observeMissingToolCallReasoning(calls, "healthy reasoning"); missing || retry {
			t.Fatalf("healthy recovery observation %d = missing:%v retry:%v, want false/false", healthy+1, missing, retry)
		}
	}

	if missing, retry := a.observeMissingToolCallReasoning(calls, ""); !missing || !retry {
		t.Fatalf("post-recovery observation = missing:%v retry:%v, want true/true", missing, retry)
	}
}

func TestRunPreservesOriginalRequiredToolCallReasoningAcrossHook(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			Reasoning: "original reasoning",
			ToolCalls: []provider.ToolCall{{
				ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`,
			}},
		},
		testutil.Turn{Text: "done"},
	)
	h := &stubHooks{hasPostLLM: true, postLLMOut: "translated display"}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{Hooks: h}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(reqs))
	}
	var toolCallAssistant provider.Message
	for _, m := range reqs[1].Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			toolCallAssistant = m
			break
		}
	}
	if toolCallAssistant.ReasoningContent != "original reasoning" {
		t.Fatalf("tool-call reasoning = %q, want original provider reasoning", toolCallAssistant.ReasoningContent)
	}
	if toolCallAssistant.ReasoningContent == "translated display" {
		t.Fatal("translated display text leaked into provider-visible tool-call reasoning")
	}
}

func TestRunStoresTransformedNonToolReasoningForToolCallOnlyProvider(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy", testutil.Turn{
		Reasoning: "original reasoning",
		Text:      "done",
	})
	h := &stubHooks{hasPostLLM: true, postLLMOut: "translated display"}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{Hooks: h}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := assistantReasoning(a.session.Messages); got != "translated display" {
		t.Fatalf("stored non-tool reasoning = %q, want transformed display text", got)
	}
}
