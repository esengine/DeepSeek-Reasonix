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

func TestBuildRequestDeepSeekMergedAssistantTurnKeepsThinkingFirst(t *testing.T) {
	c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: "enabled"}
	r := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "start"},
		{
			Role: provider.RoleAssistant, Content: "first answer",
			ToolCalls: []provider.ToolCall{{ID: "t1", Name: "read_file", Arguments: `{}`}},
		}, // reasoning lost: projection degrades this turn to plain text
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "read_file", Content: "data"},
		{
			Role: provider.RoleAssistant, Content: "second answer", ReasoningContent: "fresh thinking",
			ToolCalls: []provider.ToolCall{{ID: "t2", Name: "read_file", Arguments: `{}`}},
		},
		{Role: provider.RoleTool, ToolCallID: "t2", Name: "read_file", Content: "more data"},
		{Role: provider.RoleUser, Content: "continue"},
	}})

	// The degraded first turn and the healthy second turn merge into one
	// assistant message; its thinking block must lead.
	if len(r.Messages) != 3 {
		t.Fatalf("messages = %#v, want user/merged-assistant/user", r.Messages)
	}
	blocks := r.Messages[1].Content
	if len(blocks) != 4 ||
		blocks[0].Type != "thinking" || blocks[0].Thinking != "fresh thinking" ||
		blocks[1].Type != "text" || blocks[1].Text != "first answer" ||
		blocks[2].Type != "text" || blocks[2].Text != "second answer" ||
		blocks[3].Type != "tool_use" || blocks[3].ID != "t2" {
		t.Fatalf("merged assistant blocks = %#v, want [thinking, text, text, tool_use]", blocks)
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

func TestBuildRequestDeepSeekReplaysReasoningFromHistory(t *testing.T) {
	toolTurn := []provider.Message{
		{Role: provider.RoleUser, Content: "weather?"},
		{Role: provider.RoleAssistant, ReasoningContent: "I should call the tool.",
			ToolCalls: []provider.ToolCall{{ID: "t1", Name: "get_weather", Arguments: `{"city":"Paris"}`}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Content: "sunny"},
	}
	for _, tc := range []struct {
		name     string
		thinking string
		effort   string
	}{
		{name: "current request has no tools", thinking: "enabled", effort: "high"},
		{name: "thinking disabled after tool call", thinking: "enabled", effort: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: tc.thinking, effort: tc.effort}
			r := c.buildRequest(context.Background(), provider.Request{Messages: toolTurn})
			if len(r.Tools) != 0 {
				t.Fatalf("current request tools = %+v, want none", r.Tools)
			}
			if len(r.Messages) != 3 {
				t.Fatalf("messages = %+v, want user/assistant/user", r.Messages)
			}
			blocks := r.Messages[1].Content
			if len(blocks) != 2 || blocks[0].Type != "thinking" || blocks[0].Thinking != "I should call the tool." || blocks[1].Type != "tool_use" {
				t.Fatalf("assistant blocks = %+v, want historical thinking before tool_use", blocks)
			}
		})
	}

	t.Run("reasoning without a tool call is replayed", func(t *testing.T) {
		// DeepSeek's thinking mode requires every historical assistant turn's
		// thinking block back when the request declares tools, even for plain
		// question-answer turns (api-docs.deepseek.com/guides/thinking_mode).
		c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: "enabled", effort: "high"}
		r := c.buildRequest(context.Background(), provider.Request{
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "hello"},
				{Role: provider.RoleAssistant, Content: "hi", ReasoningContent: "scratchpad"},
			},
			Tools: []provider.ToolSchema{{Name: "get_weather"}},
		})
		blocks := r.Messages[1].Content
		if len(r.Messages) != 2 || len(blocks) != 2 || blocks[0].Type != "thinking" || blocks[0].Thinking != "scratchpad" || blocks[1].Type != "text" {
			t.Fatalf("non-tool assistant blocks = %+v, want thinking before text", r.Messages)
		}
	})
}
