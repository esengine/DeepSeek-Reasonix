package anthropic

import (
	"context"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestDeepSeekDropsUnreplayableToolActivity(t *testing.T) {
	c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: "enabled"}
	r := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "inspect the change"},
		{
			Role: provider.RoleAssistant, Content: "I checked the file.",
			ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
		},
		{Role: provider.RoleTool, ToolCallID: "read-1", Name: "read_file", Content: "package main"},
		{Role: provider.RoleUser, Content: "continue"},
	}})

	if len(r.Messages) != 3 {
		t.Fatalf("messages = %#v, want user/plain assistant/user", r.Messages)
	}
	if got := r.Messages[1].Content; len(got) != 1 || got[0].Type != "text" || got[0].Text != "I checked the file." {
		t.Fatalf("projected assistant = %#v, want visible text only", got)
	}
	for _, message := range r.Messages {
		for _, block := range message.Content {
			if block.Type == "tool_use" || block.Type == "tool_result" || block.Type == "thinking" {
				t.Fatalf("unreplayable activity reached DeepSeek wire: %#v", r.Messages)
			}
		}
	}
}

func TestDeepSeekReplayProjectionKeepsHealthyHistoryBacking(t *testing.T) {
	c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: "enabled"}
	msgs := []provider.Message{{
		Role: provider.RoleAssistant, ReasoningContent: "read it first",
		ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{}`}},
	}}
	got, changed := provider.ProjectReplaySafeMessages(c, msgs)
	if changed || len(got) != 1 || &got[0] != &msgs[0] {
		t.Fatal("healthy replay history allocated or changed")
	}
}

func TestBuildRequestNativeAnthropicKeepsItsExistingValidationPath(t *testing.T) {
	c := &client{model: "claude-sonnet", thinking: "adaptive"}
	r := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{}`}},
	}}})
	if len(r.Messages) != 2 || len(r.Messages[0].Content) != 1 || r.Messages[0].Content[0].Type != "tool_use" ||
		len(r.Messages[1].Content) != 1 || r.Messages[1].Content[0].Type != "tool_result" {
		t.Fatalf("native Anthropic history unexpectedly projected: %#v", r.Messages)
	}
}
