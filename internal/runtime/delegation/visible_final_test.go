package delegation

import (
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestRunSubAgentDoesNotReturnStalePreToolTextAfterReasoningOnlyStop(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I need to inspect the input."},
			{Type: provider.ChunkText, Text: "I'll inspect first."},
			toolCallChunk("call-1", "echo", `{"text":"input"}`),
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "The inspection is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "final findings"},
			{Type: provider.ChunkDone},
		},
	}}
	reg := tool.NewRegistry()
	reg.Add(echoTool{})

	answer, err := agent.RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, reg, sessionstore.NewSession("sys"),
		"inspect the input", agent.Options{SubagentDepth: 1}, event.Discard,
	)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if answer != "final findings" {
		t.Fatalf("answer = %q, want final findings (not stale tool preamble)", answer)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3", prov.call)
	}
	if got := lastUser(prov.requests[2]); !strings.Contains(got, "visible answer") {
		t.Fatalf("retry prompt = %q, want visible-answer nudge", got)
	}
}

func TestRunSubAgentRetriesReasoningOnlyStopForVisibleFinal(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "The analysis is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible result"},
			{Type: provider.ChunkDone},
		},
	}}
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "previous result"})

	answer, err := agent.RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, tool.NewRegistry(), sess,
		"analyze the code", agent.Options{SubagentDepth: 1}, event.Discard,
	)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if answer != "visible result" {
		t.Fatalf("answer = %q, want visible result", answer)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2", prov.call)
	}
	if got := lastUser(prov.requests[1]); !strings.Contains(got, "visible answer") {
		t.Fatalf("retry prompt = %q, want visible-answer nudge", got)
	}
}
