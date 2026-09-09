package provider

import (
	"context"
	"encoding/json"
	"testing"
)

type replayProjectionProvider struct {
	allowEmpty bool
}

func (replayProjectionProvider) Name() string { return "replay-projection-test" }

func (replayProjectionProvider) Stream(context.Context, Request) (<-chan Chunk, error) {
	panic("unexpected Stream call")
}

func (replayProjectionProvider) RequiresAssistantReasoningReplay(m Message) bool {
	return len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0
}

func (p replayProjectionProvider) AllowsEmptyReasoningFallback() bool { return p.allowEmpty }

func TestProjectReplaySafeMessagesClearsProviderActivityMetadata(t *testing.T) {
	p := replayProjectionProvider{}
	msgs := []Message{
		{Role: RoleUser, Content: "inspect"},
		{
			Role:               RoleAssistant,
			Content:            "visible answer",
			ReasoningSignature: "signature",
			ReasoningID:        "reasoning-id",
			ReasoningStatus:    "completed",
			ToolCalls:          []ToolCall{{ID: "call-1", Name: "read_file"}},
			ResponsesItems:     []json.RawMessage{json.RawMessage(`{"type":"reasoning"}`)},
			ServerSearch:       []ServerSearchCall{{ID: "search-1", Query: "query"}},
		},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "result"},
		{Role: RoleUser, Content: "continue"},
	}

	got, changed := ProjectReplaySafeMessages(p, msgs)
	if !changed {
		t.Fatal("malformed replay history was not projected")
	}
	if len(got) != 3 || got[1].Role != RoleAssistant || got[1].Content != "visible answer" {
		t.Fatalf("projection = %#v, want user/plain assistant/user", got)
	}
	plain := got[1]
	if plain.ReasoningContent != "" || plain.ReasoningSignature != "" || plain.ReasoningID != "" || plain.ReasoningStatus != "" ||
		len(plain.ToolCalls) != 0 || len(plain.ResponsesItems) != 0 || len(plain.ServerSearch) != 0 {
		t.Fatalf("provider activity metadata survived projection: %#v", plain)
	}
	if len(msgs[1].ToolCalls) != 1 || len(msgs[1].ResponsesItems) != 1 || len(msgs[1].ServerSearch) != 1 {
		t.Fatal("projection mutated canonical history")
	}
}

func TestProjectReplaySafeMessagesKeepsStableBacking(t *testing.T) {
	healthy := []Message{{
		Role: RoleAssistant, ReasoningContent: "reasoning",
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read_file"}},
	}}
	if got, changed := ProjectReplaySafeMessages(replayProjectionProvider{}, healthy); changed || &got[0] != &healthy[0] {
		t.Fatal("healthy history must retain its backing slice")
	}

	emptyFallback := []Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read_file"}},
	}}
	if got, changed := ProjectReplaySafeMessages(replayProjectionProvider{allowEmpty: true}, emptyFallback); changed || &got[0] != &emptyFallback[0] {
		t.Fatal("empty-reasoning fallback history must retain its backing slice")
	}
}

func TestProjectReasoningStrippedMessagesBypassesEmptyFallbackAfter400(t *testing.T) {
	p := replayProjectionProvider{allowEmpty: true}
	msgs := []Message{
		{Role: RoleUser, Content: "inspect"},
		{
			Role:               RoleAssistant,
			Content:            "visible answer",
			ReasoningContent:   "stale reasoning",
			ToolCalls:          []ToolCall{{ID: "call-1", Name: "read_file"}},
			ReasoningSignature: "stale signature",
		},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "result"},
		{Role: RoleUser, Content: "continue"},
	}

	got, changed := ProjectReasoningStrippedMessages(p, msgs)
	if !changed {
		t.Fatal("stale reasoning history was not changed")
	}
	if len(got) != 3 || got[1].Role != RoleAssistant || got[1].Content != "visible answer" {
		t.Fatalf("projection = %#v, want user/plain assistant/user", got)
	}
	if got[1].ReasoningContent != "" || got[1].ReasoningSignature != "" || len(got[1].ToolCalls) != 0 {
		t.Fatalf("stale assistant metadata survived projection: %#v", got[1])
	}
	if msgs[1].ReasoningContent != "stale reasoning" || len(msgs[1].ToolCalls) != 1 || len(msgs[2].Content) == 0 {
		t.Fatal("strong projection mutated canonical history")
	}
}

func TestProjectReasoningStrippedMessagesPrefixKeepsAppendedToolRound(t *testing.T) {
	p := replayProjectionProvider{}
	msgs := []Message{
		{Role: RoleUser, Content: "inspect"},
		{
			Role:             RoleAssistant,
			Content:          "old answer",
			ReasoningContent: "stale reasoning",
		},
		{Role: RoleUser, Content: "continue"},
		{
			Role:             RoleAssistant,
			ReasoningContent: "fresh reasoning",
			ToolCalls:        []ToolCall{{ID: "fresh", Name: "read_file"}},
		},
		{Role: RoleTool, ToolCallID: "fresh", Name: "read_file", Content: "fresh result"},
	}

	got, changed := ProjectReasoningStrippedMessagesPrefix(p, msgs, 3)
	if !changed {
		t.Fatal("stale prefix was not projected")
	}
	if len(got) != len(msgs) {
		t.Fatalf("projection length = %d, want %d", len(got), len(msgs))
	}
	if got[1].ReasoningContent != "" {
		t.Fatalf("stale prefix reasoning survived: %#v", got[1])
	}
	if got[3].ReasoningContent != "fresh reasoning" || len(got[3].ToolCalls) != 1 || got[4].Role != RoleTool {
		t.Fatalf("appended tool round changed: %#v", got)
	}
}
