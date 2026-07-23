package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestEmbedsImagesForVisionModel(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "what is this", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	parts, ok := req.Messages[0].Content.([]chatContentPart)
	if !ok {
		t.Fatalf("vision user content = %T, want []chatContentPart", req.Messages[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("parts = %+v, want [text, image_url]", parts)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url = %+v, want the data URL", parts[1].ImageURL)
	}
	body, _ := json.Marshal(req.Messages[0])
	if !strings.Contains(string(body), `"type":"image_url"`) {
		t.Errorf("serialized content missing image_url part: %s", body)
	}
}

func TestBuildRequestSkipsImagesWithoutVision(t *testing.T) {
	c := &client{model: "deepseek-v4"} // vision unset
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "ignore the image", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	if s, ok := req.Messages[0].Content.(string); !ok || s != "ignore the image" {
		t.Fatalf("non-vision content = %#v, want plain string", req.Messages[0].Content)
	}
}

func TestImageURLDetailFromConfig(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true, visionDetail: "low"}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "x", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	parts := req.Messages[0].Content.([]chatContentPart)
	if parts[1].ImageURL.Detail != "low" {
		t.Fatalf("detail = %q, want low", parts[1].ImageURL.Detail)
	}
}

func TestImageURLDetailOmittedByDefault(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "x", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	body, _ := json.Marshal(req.Messages[0].Content.([]chatContentPart)[1])
	if strings.Contains(string(body), "detail") {
		t.Errorf("detail must be omitted when unset: %s", body)
	}
}

// Tool-result images can't ride in the tool message itself (the OpenAI API
// accepts only text parts under role "tool"), so buildRequest injects them as
// a user message after the turn's full run of tool results.
func TestBuildRequestInjectsToolImagesAsUserMessage(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "screenshot please"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "c1", Name: "shot", Arguments: "{}"},
				{ID: "c2", Name: "shot", Arguments: "{}"},
			}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,AAAA"}},
			{Role: provider.RoleTool, ToolCallID: "c2", Name: "shot", Content: "no image"},
			{Role: provider.RoleUser, Content: "and?"},
		},
	})
	if len(req.Messages) != 6 {
		t.Fatalf("got %d messages, want 6 (images injected after the tool run)", len(req.Messages))
	}
	for i, m := range req.Messages[2:4] {
		if _, ok := m.Content.(string); !ok || m.Role != "tool" {
			t.Fatalf("message %d = %+v, want tool message with plain string content", i+2, m)
		}
	}
	inj := req.Messages[4]
	if inj.Role != "user" {
		t.Fatalf("injected message role = %q, want user between tool run and next turn", inj.Role)
	}
	parts, ok := inj.Content.([]chatContentPart)
	if !ok || len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("injected content = %#v, want [text, image_url]", inj.Content)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url = %+v, want the tool image data URL", parts[1].ImageURL)
	}
	if req.Messages[5].Content != "and?" {
		t.Fatalf("trailing user message displaced: %+v", req.Messages[5])
	}
}

func TestBuildRequestFlushesTrailingToolImages(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" {
		t.Fatalf("last message = %+v, want the injected image user message", last)
	}
	if _, ok := last.Content.([]chatContentPart); !ok {
		t.Fatalf("last content = %#v, want content parts", last.Content)
	}
}

func TestBuildRequestSkipsToolImagesWithoutVision(t *testing.T) {
	c := &client{model: "deepseek-v4"} // vision unset
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	if len(req.Messages) != 3 {
		t.Fatalf("got %d messages, want 3 (no injection without vision)", len(req.Messages))
	}
	if s, ok := req.Messages[2].Content.(string); !ok || s != "[image: image/png]" {
		t.Fatalf("tool content = %#v, want the plain placeholder string", req.Messages[2].Content)
	}
}
