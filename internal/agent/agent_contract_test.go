package agent

// Core agent-loop contract tests (agent-core simplification baseline). Each
// test pins one behavior the simplified loop promises; suites covering the
// remaining contract items are listed in docs/AGENT_CORE_SIMPLIFICATION.md.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestContractCleanFinalMakesOneModelRequest pins the executor-only happy
// path: one user turn, one provider request, no host follow-ups.
func TestContractCleanFinalMakesOneModelRequest(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "the answer"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "question"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want exactly one for a clean final", len(prov.requests))
	}
	if got := lastAssistantContent(a.sess.conversation); got != "the answer" {
		t.Fatalf("last assistant content = %q, want the final answer", got)
	}
}

// TestContractToolCallAdvancesToNextStep pins the tool-loop shape: a tool-call
// round executes and the loop continues to a second request for the final.
func TestContractToolCallAdvancesToNextStep(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "all set"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := mp.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want tool round + final", got)
	}
	if got := lastToolResult(a.Session(), "echo"); got == "" {
		t.Fatal("tool result missing from session; tool round did not execute")
	}
	if got := lastAssistantContent(a.sess.conversation); got != "all set" {
		t.Fatalf("last assistant content = %q, want the post-tool final", got)
	}
}

// TestContractThinkingSurvivesUnifiedRetry pins that the EMPTY_RESPONSE retry
// replays the same frozen request (thinking never disabled or reshaped) and
// that recovered reasoning stays on the committed assistant turn.
func TestContractThinkingSurvivesUnifiedRetry(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkDone}},
		{
			{Type: provider.ChunkReasoning, Text: "need to think"},
			{Type: provider.ChunkText, Text: "the answer"},
			{Type: provider.ChunkDone},
		},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "question"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 2 || !reflect.DeepEqual(prov.requests[0], prov.requests[1]) {
		t.Fatalf("retry must replay the frozen request verbatim:\nfirst=%#v\nsecond=%#v", prov.requests[0], prov.requests[1])
	}
	msgs := a.sess.conversation.Snapshot()
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.ReasoningContent != "need to think" {
		t.Fatalf("committed assistant turn lost reasoning: %+v", last)
	}
}

// TestContractNoLongLivedFallbackStateAfterRetryExhaustion pins that exhausted
// protocol retries fail the turn without installing any persistent degraded
// mode: the next turn runs a normal single-request loop.
func TestContractNoLongLivedFallbackStateAfterRetryExhaustion(t *testing.T) {
	turns := [][]provider.Chunk{}
	for range maxSamplingAttempts {
		turns = append(turns, []provider.Chunk{{Type: provider.ChunkDone}})
	}
	turns = append(turns, []provider.Chunk{{Type: provider.ChunkText, Text: "recovered"}, {Type: provider.ChunkDone}})
	prov := &scriptedProvider{name: "p", turns: turns}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "question"); err == nil {
		t.Fatal("exhausted empty-response retries must fail the turn")
	}
	for _, m := range a.sess.conversation.Snapshot() {
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			t.Fatalf("failed turn committed assistant content %q", m.Content)
		}
	}
	if err := a.Run(context.Background(), "try again"); err != nil {
		t.Fatalf("second Run after exhausted retries: %v", err)
	}
	if got := len(prov.requests) - maxSamplingAttempts; got != 1 {
		t.Fatalf("second turn made %d requests, want a normal single request (no fallback mode)", got)
	}
	if got := lastAssistantContent(a.sess.conversation); got != "recovered" {
		t.Fatalf("last assistant content = %q, want the clean recovery answer", got)
	}
}

// TestContractCleanFinalAddsNoSyntheticContinuation pins that a direct Run
// ends at the first clean final: no host-generated user message may appear in
// the session, whatever the model text promises.
func TestContractCleanFinalAddsNoSyntheticContinuation(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "I will handle it next round."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "question"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want one; a promised-later answer must not buy a continuation", len(prov.requests))
	}
	for _, prefix := range []string{StandardTodoContinuationPrefix, "visible answer", executorHandoffMarker} {
		if sessionHasUserMessageContaining(a.sess.conversation, prefix) {
			t.Fatalf("session contains synthetic continuation prompt %q", prefix)
		}
	}
}
