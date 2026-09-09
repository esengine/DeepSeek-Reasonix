package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type strictNoWarningReasoningProvider struct{ *testutil.MockProvider }

func (p strictNoWarningReasoningProvider) RequiresToolCallReasoning() bool      { return true }
func (p strictNoWarningReasoningProvider) WarnOnMissingToolCallReasoning() bool { return false }

func TestStrictNoWarningReplayDoesNotLeakSpeculativeEvents(t *testing.T) {
	missingTurn := func(id string) testutil.Turn {
		call := provider.ToolCall{ID: id, Name: "echo", Arguments: `{"text":"must not run"}`}
		return testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "speculative"},
			{Type: provider.ChunkToolCallStart, ToolCall: &call},
			{Type: provider.ChunkToolCall, ToolCall: &call},
			{Type: provider.ChunkDone},
		}}
	}
	providerMock := testutil.NewMock("strict-no-warning", missingTurn("c1"), missingTurn("c2"))
	sink := &recordSink{}
	agent := New(strictNoWarningReasoningProvider{providerMock}, echoRegistry(), NewSession(""), Options{}, sink)

	var replayErr *ReasoningReplayError
	if err := agent.Run(withNoClosedLoop(context.Background()), "go"); !errors.As(err, &replayErr) {
		t.Fatalf("Run error = %v, want ReasoningReplayError", err)
	}
	if got := providerMock.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want malformed turn plus one exact retry", got)
	}
	for _, kind := range []event.Kind{event.ToolDispatch, event.ToolResult, event.Text, event.Message} {
		if got := len(sink.kinds(kind)); got != 0 {
			t.Fatalf("speculative %v events = %d, want 0", kind, got)
		}
	}
}

func thinkingReplay400Error() error {
	return provider.ParseReasoningReplayError(&provider.APIError{
		Provider: "strict-replay", Status: 400,
		Body: `{"error":{"message":"The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API"}}`,
	})
}

func reasoningReplaySeededSession() *Session {
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "earlier"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer", ReasoningContent: "stale thinking"})
	return session
}

func TestReasoningReplay400RepairsProjectionAndRetriesOnce(t *testing.T) {
	mp := testutil.NewMock("strict-replay",
		testutil.ErrorTurn(thinkingReplay400Error()),
		testutil.Turn{Text: "done"},
		testutil.Turn{Text: "again done"},
	)
	sink := &recordSink{}
	session := reasoningReplaySeededSession()
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), session, Options{}, sink)

	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := mp.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want rejected attempt plus one repair retry", got)
	}
	requests := mp.Requests()
	var firstReasoning, secondReasoning int
	for _, m := range requests[0].Messages {
		if m.ReasoningContent != "" {
			firstReasoning++
		}
	}
	for _, m := range requests[1].Messages {
		if m.ReasoningContent != "" {
			secondReasoning++
		}
	}
	if firstReasoning != 1 || secondReasoning != 0 {
		t.Fatalf("reasoning in attempts = %d then %d, want the repair retry stripped", firstReasoning, secondReasoning)
	}
	// The frozen request may change only in Messages; everything else is
	// byte-identical to the rejected attempt.
	strippedTools := requests[1]
	strippedTools.Messages = requests[0].Messages
	if !reflect.DeepEqual(requests[0], strippedTools) {
		t.Fatalf("repair retry changed more than Messages:\nfirst=%+v\nretry=%+v", requests[0], requests[1])
	}
	// Canonical history is never modified by the provider-visible projection.
	for _, m := range session.Snapshot() {
		if m.Role == provider.RoleAssistant && m.Content == "old answer" && m.ReasoningContent != "stale thinking" {
			t.Fatalf("canonical history lost its reasoning: %+v", m)
		}
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Detected); got != 1 {
		t.Fatalf("reasoning_replay_400_detected audits = %d, want 1", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Recovered); got != 1 {
		t.Fatalf("reasoning_replay_400_recovered audits = %d, want 1", got)
	}
	var repairNotices int
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodeReasoningReplayRepair {
			repairNotices++
			if e.Level != event.LevelWarn {
				t.Fatalf("repair notice level = %v, want warn", e.Level)
			}
		}
	}
	if repairNotices != 1 {
		t.Fatalf("repair notices = %d, want 1", repairNotices)
	}

	// The strong projection stays active for the rest of the conversation.
	if err := a.Run(withNoClosedLoop(context.Background()), "again"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := mp.CallCount(); got != 3 {
		t.Fatalf("provider calls = %d, want no fresh 400 on the next run", got)
	}
	for _, m := range mp.Requests()[2].Messages {
		if m.ReasoningContent != "" {
			t.Fatalf("later request still carries reasoning under strong projection: %+v", m)
		}
	}
}

func TestReasoningReplay400KeepsNewToolRoundOutsideStrongProjection(t *testing.T) {
	call := provider.ToolCall{ID: "fresh", Name: "echo", Arguments: `{"text":"hello"}`}
	mp := testutil.NewMock("strict-replay",
		testutil.ErrorTurn(thinkingReplay400Error()),
		testutil.Turn{Text: "repaired"},
		testutil.Turn{Reasoning: "fresh reasoning", ToolCalls: []provider.ToolCall{call}},
		testutil.Turn{Text: "fresh final"},
	)
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), reasoningReplaySeededSession(), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "first"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "second"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	requests := mp.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests = %d, want repair plus a two-step follow-up", len(requests))
	}
	var sawFreshToolRound bool
	for _, message := range requests[3].Messages {
		if len(message.ToolCalls) > 0 || message.Role == provider.RoleTool {
			sawFreshToolRound = true
			break
		}
	}
	if !sawFreshToolRound {
		t.Fatal("strong projection dropped the new tool round from the follow-up request")
	}
}

func TestReasoningReplay400ProjectsAllStaleToolRoundsBeforeFreshRound(t *testing.T) {
	old1 := provider.ToolCall{ID: "old-1", Name: "echo", Arguments: `{"text":"old one"}`}
	old2 := provider.ToolCall{ID: "old-2", Name: "echo", Arguments: `{"text":"old two"}`}
	old3 := provider.ToolCall{ID: "old-3", Name: "echo", Arguments: `{"text":"old three"}`}
	fresh := provider.ToolCall{ID: "fresh", Name: "echo", Arguments: `{"text":"fresh"}`}
	mp := testutil.NewMock("strict-replay",
		testutil.ErrorTurn(thinkingReplay400Error()),
		testutil.Turn{Text: "repaired"},
		testutil.Turn{Reasoning: "fresh reasoning", ToolCalls: []provider.ToolCall{fresh}},
		testutil.Turn{Text: "fresh final"},
	)
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old one", ReasoningContent: "stale one", ToolCalls: []provider.ToolCall{old1}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: old1.ID, Name: old1.Name, Content: "old result one"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old two", ReasoningContent: "stale two", ToolCalls: []provider.ToolCall{old2}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: old2.ID, Name: old2.Name, Content: "old result two"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "third"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old three", ReasoningContent: "stale three", ToolCalls: []provider.ToolCall{old3}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: old3.ID, Name: old3.Name, Content: "old result three"})
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), session, Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "repair"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "fresh"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	requests := mp.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests = %d, want repair plus a two-step follow-up", len(requests))
	}
	for _, message := range requests[3].Messages {
		if message.ReasoningContent == "stale one" || message.ReasoningContent == "stale two" || message.ReasoningContent == "stale three" {
			t.Fatalf("stale reasoning survived prefix projection: %+v", message)
		}
	}
	var sawFreshToolRound bool
	for _, message := range requests[3].Messages {
		if len(message.ToolCalls) > 0 || message.Role == provider.RoleTool {
			sawFreshToolRound = true
			break
		}
	}
	if !sawFreshToolRound {
		t.Fatal("strong projection dropped the fresh tool round")
	}
}

func TestReasoningReplayAnchorMissFallsBackToNormalProjection(t *testing.T) {
	call := provider.ToolCall{ID: "stale", Name: "echo", Arguments: `{"text":"old"}`}
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "old"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer", ToolCalls: []provider.ToolCall{call}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: "old result"})
	a := New(strictAssistantReasoningProvider{testutil.NewMock("strict-replay")}, echoRegistry(), session, Options{}, event.Discard)
	a.sess.reasoningReplayStrongProjection = 2
	a.sess.reasoningReplayStrongProjectionAnchor = "anchor no longer present"

	got := a.providerProjectionMessages(modelInputMessages(session.Snapshot()))
	if a.sess.reasoningReplayStrongProjection != 0 || a.sess.reasoningReplayStrongProjectionAnchor != "" {
		t.Fatalf("stale strong projection survived anchor miss: cutoff=%d anchor=%q", a.sess.reasoningReplayStrongProjection, a.sess.reasoningReplayStrongProjectionAnchor)
	}
	for _, message := range got {
		if len(message.ToolCalls) > 0 || message.Role == provider.RoleTool {
			t.Fatalf("normal projection did not remove unreplayable tool history: %+v", got)
		}
	}
}

func TestReasoningReplayProjectionInvalidationClearsOverlay(t *testing.T) {
	a := New(nil, echoRegistry(), NewSession("system"), Options{}, event.Discard)
	a.sess.reasoningReplayStrongProjection = 7
	a.sess.reasoningReplayStrongProjectionAnchor = "anchor"

	a.InvalidateProjection()
	if a.sess.reasoningReplayStrongProjection != 0 || a.sess.reasoningReplayStrongProjectionAnchor != "" {
		t.Fatalf("projection invalidation retained repair overlay: cutoff=%d anchor=%q", a.sess.reasoningReplayStrongProjection, a.sess.reasoningReplayStrongProjectionAnchor)
	}
}

func TestReasoningReplay400RepairExhaustionStaysTerminal(t *testing.T) {
	mp := testutil.NewMock("strict-replay",
		testutil.ErrorTurn(thinkingReplay400Error()),
		testutil.ErrorTurn(thinkingReplay400Error()),
		testutil.Turn{Text: "unreachable"},
	)
	sink := &recordSink{}
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), reasoningReplaySeededSession(), Options{}, sink)

	err := a.Run(withNoClosedLoop(context.Background()), "next")
	var replayErr *provider.ReasoningReplayError
	if !errors.As(err, &replayErr) {
		t.Fatalf("Run error = %v, want ReasoningReplayError", err)
	}
	if got := mp.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want exactly one repair retry", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Detected); got != 1 {
		t.Fatalf("reasoning_replay_400_detected audits = %d, want 1", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Recovered); got != 0 {
		t.Fatalf("reasoning_replay_400_recovered audits = %d, want 0 after a failed repair", got)
	}
}

func TestNonReplay400DoesNotTriggerReasoningRepair(t *testing.T) {
	mp := testutil.NewMock("strict-replay",
		testutil.ErrorTurn(&provider.APIError{Provider: "strict-replay", Status: 400, Body: `{"error":{"message":"invalid request: unknown tool"}}`}),
		testutil.Turn{Text: "unreachable"},
	)
	sink := &recordSink{}
	a := New(strictAssistantReasoningProvider{mp}, echoRegistry(), reasoningReplaySeededSession(), Options{}, sink)

	err := a.Run(withNoClosedLoop(context.Background()), "next")
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Run error = %v, want the raw APIError", err)
	}
	if replayErr := provider.AsReasoningReplayError(err); replayErr != nil {
		t.Fatalf("unrelated 400 misclassified as reasoning replay: %v", replayErr)
	}
	if got := mp.CallCount(); got != 1 {
		t.Fatalf("provider calls = %d, want no retry for an unrelated 400", got)
	}
	if got := sink.recoveryCount(event.ProtocolRecoveryReasoningReplay400Detected); got != 0 {
		t.Fatalf("reasoning_replay_400_detected audits = %d, want 0", got)
	}
}
