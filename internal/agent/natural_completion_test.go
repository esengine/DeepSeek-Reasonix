package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestNaturalCompletionAcceptsVisibleAnswerInOneRound(t *testing.T) {
	prov := &scriptedProvider{name: "natural-completion", turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "The requested work is complete."},
		{Type: provider.ChunkDone},
	}}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want one natural completion round", prov.call)
	}
	if len(prov.requests) != 1 || len(prov.requests[0].Tools) != 0 {
		t.Fatalf("provider tools = %#v, want no finish schema", prov.requests)
	}
}

func TestNaturalCompletionReplaysLegacyFinishHistoryWithoutSchema(t *testing.T) {
	sess := NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "legacy request"})
	sess.Add(provider.Message{
		Role:    provider.RoleAssistant,
		Content: "Legacy answer.",
		ToolCalls: []provider.ToolCall{{
			ID: "legacy-finish", Name: "finish", Arguments: `{"outcome":"completed"}`,
		}},
	})
	sess.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: "legacy-finish", Name: "finish",
		Content: "Turn finalization accepted by the host.",
	})
	prov := &scriptedProvider{name: "natural-completion-legacy", turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "The next turn also ends naturally."},
		{Type: provider.ChunkDone},
	}}}
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)

	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != 1 || len(prov.requests[0].Tools) != 0 {
		t.Fatalf("provider tools = %#v, want legacy replay without finish schema", prov.requests)
	}
	var sawCall, sawResult bool
	for _, message := range prov.requests[0].Messages {
		for _, call := range message.ToolCalls {
			if call.ID == "legacy-finish" && call.Name == "finish" {
				sawCall = true
			}
		}
		if message.Role == provider.RoleTool && message.ToolCallID == "legacy-finish" && message.Name == "finish" {
			sawResult = true
		}
	}
	if !sawCall || !sawResult {
		t.Fatalf("legacy finish pair was not preserved in provider history: call=%t result=%t", sawCall, sawResult)
	}
}
