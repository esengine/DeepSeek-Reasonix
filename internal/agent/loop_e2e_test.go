package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type toolCallReasoningRequiredProvider struct {
	*testutil.MockProvider
}

func (p toolCallReasoningRequiredProvider) RequiresToolCallReasoning() bool { return true }

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

// TestRunWarnsAndContinuesOnMissingToolCallReasoning: a DeepSeek thinking-mode
// tool_calls turn arriving without reasoning is a quality degradation, not a
// failure — the turn is saved, the loop continues to completion, and the user
// sees a single warn notice. Missing reasoning tends to repeat on every round
// once it starts (endpoint-conditional behavior, seen on the official API too),
// so later rounds with the same shape must stay silent instead of flooding the
// transcript (#6259). The wire layer keeps the replay valid by always
// serializing the reasoning_content key on such turns.
func TestRunWarnsAndContinuesOnMissingToolCallReasoning(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"again"}`}}},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var savedToolTurns int
	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			savedToolTurns++
		}
	}
	if savedToolTurns != 2 {
		t.Fatalf("tool-call turns saved = %d, want 2 despite missing reasoning, session=%+v", savedToolTurns, a.Session().Messages)
	}
	var warns int
	for _, e := range sink.kinds(event.Notice) {
		if e.Level == event.LevelWarn && strings.Contains(e.Text, "without reasoning_content") {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("missing-reasoning warn notices = %d, want exactly 1 (first round warns, repeats stay silent)", warns)
	}
}

// TestSetSessionRearmsMissingToolCallReasoningWarn: the once-per-session dedupe
// is scoped to the conversation — swapping in a different session (resume/new)
// must re-arm the notice so the fresh conversation still gets its one warning.
func TestSetSessionRearmsMissingToolCallReasoningWarn(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"hi"}`}}},
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
	var warns int
	for _, e := range sink.kinds(event.Notice) {
		if e.Level == event.LevelWarn && strings.Contains(e.Text, "without reasoning_content") {
			warns++
		}
	}
	if warns != 2 {
		t.Fatalf("warn notices across two sessions = %d, want 2 (SetSession re-arms the dedupe)", warns)
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
