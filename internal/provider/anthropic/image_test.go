package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func TestBuildRequestEmbedsImageBlockForVisionModel(t *testing.T) {
	c := &client{model: "claude-opus-4-8", vision: true}
	req := c.buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/jpeg;base64,ZZZZ"}},
		},
	})
	blocks := req.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "image" {
		t.Fatalf("blocks = %+v, want [text, image]", blocks)
	}
	src := blocks[1].Source
	if src == nil || src.Type != "base64" || src.MediaType != "image/jpeg" || src.Data != "ZZZZ" {
		t.Fatalf("image source = %+v, want base64 / image/jpeg / ZZZZ", src)
	}
}

func TestModelInfoEnablesImageWireSerialization(t *testing.T) {
	p, err := New(provider.Config{
		Name: "catalog", BaseURL: "https://example.test", Model: "vision",
		ModelInfo: &provider.ModelInfo{ID: "vision", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.vision {
		t.Fatal("model metadata should enable image wire serialization")
	}
	req := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}})
	if len(req.Messages[0].Content) != 2 || req.Messages[0].Content[1].Type != "image" {
		t.Fatalf("content = %#v, want text + image blocks", req.Messages[0].Content)
	}
}

func TestBuildRequestSkipsImageBlockWithoutVision(t *testing.T) {
	c := &client{model: "claude-opus-4-8"} // vision unset
	req := c.buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/jpeg;base64,ZZZZ"}},
		},
	})
	blocks := req.Messages[0].Content
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatalf("blocks = %+v, want [text] only when vision is off", blocks)
	}
}

func TestOfficialDeepSeekVisionSKUEmbedsUserImages(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   openai.OfficialDeepSeekVisionModel,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.vision {
		t.Fatal("pinned official DeepSeek vision SKU must enable user image serialization")
	}
	req := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe",
		Images: []string{"data:image/jpeg;base64,ZZZZ"},
	}}})
	blocks := req.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "image" {
		t.Fatalf("blocks = %+v, want [text, image]", blocks)
	}
	src := blocks[1].Source
	if src == nil || src.Type != "base64" || src.MediaType != "image/jpeg" || src.Data != "ZZZZ" {
		t.Fatalf("image source = %+v", src)
	}
}

func TestOfficialRequestURLImageHardLimit(t *testing.T) {
	p, err := New(provider.Config{BaseURL: "https://relay.test", Model: "deepseek-v4-flash", Extra: map[string]any{"request_url": "https://api.deepseek.com/anthropic/v1/messages", "vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(p.(*client).buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}}))
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("official request URL leaked image: %s %v", body, err)
	}
}

func TestOfficialVisionExplicitOffRespectsResolvedMetadata(t *testing.T) {
	p, err := New(provider.Config{BaseURL: "https://api.deepseek.com/anthropic", Model: openai.OfficialDeepSeekVisionModel, Extra: map[string]any{"vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(p.(*client).buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}}))
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("explicit off leaked image: %s %v", body, err)
	}
	if p.(provider.ModelInfoProvider).ModelInfo().SupportsInput(provider.ModalityImage) {
		t.Fatal("metadata disagrees with serializer")
	}
}

func TestOfficialDeepSeekVisionSKUEmbedsURLAndFileID(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   openai.OfficialDeepSeekVisionModel,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	req := c.buildRequest(context.Background(), provider.Request{Messages: []provider.Message{{
		Role:    provider.RoleUser,
		Content: "describe",
		Images: []string{
			"https://cdn.example.com/cat.png",
			"file-api-0a1b2c3d4e5f6071",
		},
	}}})
	blocks := req.Messages[0].Content
	if len(blocks) != 3 || blocks[1].Type != "image" || blocks[2].Type != "image" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[1].Source == nil || blocks[1].Source.Type != "url" || blocks[1].Source.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("url source = %+v", blocks[1].Source)
	}
	if blocks[2].Source == nil || blocks[2].Source.Type != "file" || blocks[2].Source.FileID != "file-api-0a1b2c3d4e5f6071" {
		t.Fatalf("file source = %+v", blocks[2].Source)
	}
}

func TestOfficialDeepSeekVisionSKUOmitsToolImages(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   openai.OfficialDeepSeekVisionModel,
		Extra:   map[string]any{"vision": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	req := c.buildRequest(context.Background(), provider.Request{Messages: toolMessages([]string{"data:image/png;base64,QUFB"})})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"type":"image"`) || strings.Contains(string(body), "QUFB") {
		t.Fatalf("official DeepSeek vision SKU leaked tool image payload: %s", body)
	}
}

func TestOfficialDeepSeekIgnoresVisionMetadata(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   "deepseek-v4-pro",
		Extra:   map[string]any{"vision": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.vision {
		t.Fatal("official DeepSeek Anthropic endpoint must ignore vision metadata")
	}
	req := c.buildRequest(context.Background(), provider.Request{Messages: append(
		[]provider.Message{{
			Role: provider.RoleUser, Content: "describe",
			Images: []string{"data:image/jpeg;base64,ZZZZ"},
		}},
		toolMessages([]string{"data:image/png;base64,QUFB"})...,
	)})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(body), `"type":"image"`) || strings.Contains(string(body), "ZZZZ") || strings.Contains(string(body), "QUFB") {
		t.Fatalf("official DeepSeek Anthropic request leaked image payload: %s", body)
	}
}

func TestOfficialDeepSeekImageMetadataMatchesTextOnlyWireBytes(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   "deepseek-v4-pro",
		Extra:   map[string]any{"vision": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	plain := []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "vision result"},
	}
	withImages := append([]provider.Message(nil), plain...)
	withImages[0].Images = []string{"data:image/png;base64," + strings.Repeat("QUFB", 20_000)}
	withImages[2].Images = []string{"data:image/png;base64,VE9PTA=="}

	plainBody, err := json.Marshal(c.buildRequest(context.Background(), provider.Request{Messages: plain}))
	if err != nil {
		t.Fatalf("marshal plain request: %v", err)
	}
	imageBody, err := json.Marshal(c.buildRequest(context.Background(), provider.Request{Messages: withImages}))
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	if !bytes.Equal(imageBody, plainBody) {
		t.Fatalf("official DeepSeek Anthropic image metadata changed provider-visible bytes:\nplain: %s\nimage: %s", plainBody, imageBody)
	}
}

// toolMessages is a paired history whose tool result carries an image: the
// shape parseToolResult produces for an MCP screenshot tool.
func toolMessages(images []string) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "screenshot please"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: images},
	}
}

func TestBuildRequestEmbedsToolResultImagesForVisionModel(t *testing.T) {
	c := &client{model: "claude-opus-4-8", vision: true}
	req := c.buildRequest(context.Background(), provider.Request{Messages: toolMessages([]string{"data:image/png;base64,QUFB"})})
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Type != "tool_result" {
		t.Fatalf("last message = %+v, want a single tool_result block", last)
	}
	blocks, ok := last.Content[0].Content.([]contentBlock)
	if !ok {
		t.Fatalf("tool_result content = %T, want []contentBlock", last.Content[0].Content)
	}
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "image" {
		t.Fatalf("tool_result blocks = %+v, want [text, image]", blocks)
	}
	if blocks[0].Text != "[image: image/png]" {
		t.Fatalf("text block = %q, want the placeholder text", blocks[0].Text)
	}
	src := blocks[1].Source
	if src == nil || src.Type != "base64" || src.MediaType != "image/png" || src.Data != "QUFB" {
		t.Fatalf("image source = %+v, want base64 / image/png / QUFB", src)
	}
}

func TestBuildRequestDropsToolResultImagesWithoutVision(t *testing.T) {
	c := &client{model: "claude-opus-4-8"} // vision unset
	req := c.buildRequest(context.Background(), provider.Request{Messages: toolMessages([]string{"data:image/png;base64,QUFB"})})
	last := req.Messages[len(req.Messages)-1]
	if s, ok := last.Content[0].Content.(string); !ok || s != "[image: image/png]" {
		t.Fatalf("non-vision tool_result content = %#v, want the plain placeholder string", last.Content[0].Content)
	}
}

// A text-only tool result must keep serializing exactly as before the image
// channel existed: plain string content, no array — the prompt-cache prefix of
// existing sessions depends on those bytes.
func TestBuildRequestToolResultTextOnlyKeepsStringContent(t *testing.T) {
	c := &client{model: "claude-opus-4-8", vision: true}
	msgs := toolMessages(nil)
	msgs[2].Content = "plain output"
	// Trailing user turn merges after the tool_result in the same user message
	// and takes the cache breakpoint, so the tool_result block keeps its
	// pre-image-channel bytes.
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "next"})
	req := c.buildRequest(context.Background(), provider.Request{Messages: msgs})
	last := req.Messages[len(req.Messages)-1]
	if len(last.Content) != 2 || last.Content[0].Type != "tool_result" {
		t.Fatalf("last message blocks = %+v, want [tool_result, text]", last.Content)
	}
	if s, ok := last.Content[0].Content.(string); !ok || s != "plain output" {
		t.Fatalf("tool_result content = %#v, want plain string", last.Content[0].Content)
	}
	body, err := json.Marshal(last.Content[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"tool_result","tool_use_id":"c1","content":"plain output"}`
	if string(body) != want {
		t.Fatalf("serialized tool_result = %s, want %s", body, want)
	}
}
