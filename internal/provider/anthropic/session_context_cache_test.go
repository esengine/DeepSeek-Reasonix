package anthropic

import (
	"context"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
)

func TestBuildRequestRestoresSessionContextBlockAndUsesThreeBreakpoints(t *testing.T) {
	snapshot := sessioncontext.Build(sessioncontext.Sections{
		Environment: "go/linux", Workspace: `Current workspace: "/work"`, SkillsCatalog: "```\nreview — Review code\n```",
	})
	c := &client{model: "claude-opus-4-8"}
	r := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "stable system"},
		{Role: provider.RoleUser, Content: snapshot.Content},
		{Role: provider.RoleUser, Content: "real request"},
	}})
	if len(r.System) != 1 || r.System[0].CacheControl == nil {
		t.Fatalf("system breakpoint = %+v", r.System)
	}
	if len(r.Messages) != 1 || len(r.Messages[0].Content) != 2 {
		t.Fatalf("strict-role user blocks = %+v", r.Messages)
	}
	contextBlock, requestBlock := r.Messages[0].Content[0], r.Messages[0].Content[1]
	if contextBlock.Text != snapshot.Content || contextBlock.CacheControl == nil {
		t.Fatalf("context block = %+v", contextBlock)
	}
	if requestBlock.Text != "real request" || requestBlock.CacheControl == nil {
		t.Fatalf("last-message block = %+v", requestBlock)
	}
	breakpoints := 0
	for _, block := range r.System {
		if block.CacheControl != nil {
			breakpoints++
		}
	}
	for _, message := range r.Messages {
		for _, block := range message.Content {
			if block.CacheControl != nil {
				breakpoints++
			}
		}
	}
	if breakpoints != 3 {
		t.Fatalf("cache breakpoints = %d, want 3", breakpoints)
	}
}

func TestBuildRequestDeepSeekSessionContextHasNoCacheControl(t *testing.T) {
	snapshot := sessioncontext.Build(sessioncontext.Sections{Workspace: "/work"})
	r := (&client{model: "deepseek-v4", deepseek: true}).buildRequest(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "stable system"},
		{Role: provider.RoleUser, Content: snapshot.Content},
		{Role: provider.RoleUser, Content: "real request"},
	}})
	for _, block := range r.System {
		if block.CacheControl != nil {
			t.Fatalf("DeepSeek system block has cache_control: %+v", block)
		}
	}
	for _, message := range r.Messages {
		for _, block := range message.Content {
			if block.CacheControl != nil {
				t.Fatalf("DeepSeek message block has cache_control: %+v", block)
			}
		}
	}
}
