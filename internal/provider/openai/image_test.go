package openai

import (
	"bytes"
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

func TestModelInfoEnablesImageWireSerialization(t *testing.T) {
	p, err := New(provider.Config{
		Name: "catalog", BaseURL: "https://example.test/v1", Model: "kimi-k3",
		ModelInfo: &provider.ModelInfo{ID: "kimi-k3", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.vision {
		t.Fatal("model metadata should enable image wire serialization")
	}
	req := c.buildRequest(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}})
	if _, ok := req.Messages[0].Content.([]chatContentPart); !ok {
		t.Fatalf("content = %#v, want image content parts", req.Messages[0].Content)
	}
}

func TestBuildRequestEmbedsOfficialDeepSeekImageURLAndFileID(t *testing.T) {
	c := &client{model: OfficialDeepSeekVisionModel, vision: true, deepseek: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "what is this",
			Images: []string{
				"https://cdn.example.com/cat.png",
				"file-api-0a1b2c3d4e5f6071",
			},
		}},
	})
	parts, ok := req.Messages[0].Content.([]chatContentPart)
	if !ok || len(parts) != 3 {
		t.Fatalf("parts = %#v", req.Messages[0].Content)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("url part = %+v", parts[1])
	}
	if parts[2].Type != "file" || parts[2].FileID != "file-api-0a1b2c3d4e5f6071" {
		t.Fatalf("file part = %+v", parts[2])
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

func TestOfficialDeepSeekProviderWideVisionInputMatchesTextOnlyRequest(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-pro",
		Extra:   map[string]any{"vision": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.vision {
		t.Fatal("official DeepSeek endpoint must ignore stale vision=true config")
	}

	textOnly := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe this image",
	}}}
	withImage := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe this image",
		Images: []string{"data:image/png;base64," + strings.Repeat("QUFB", 20_000)},
	}}}
	textBody, err := json.Marshal(c.buildRequest(textOnly))
	if err != nil {
		t.Fatalf("marshal text request: %v", err)
	}
	imageBody, err := json.Marshal(c.buildRequest(withImage))
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	if !bytes.Equal(imageBody, textBody) {
		t.Fatalf("official DeepSeek image request changed provider-visible bytes:\ntext:  %s\nimage: %s", textBody, imageBody)
	}
}

func TestOfficialDeepSeekExplicitModelVisionInputMatchesTextOnlyRequest(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v5-vision",
		Extra: map[string]any{
			"vision":                true,
			"vision_model_explicit": true,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.vision {
		t.Fatal("explicit model-scoped vision must not bypass the official DeepSeek endpoint guard")
	}

	textOnly := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe",
	}}}
	withImage := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe",
		Images: []string{"data:image/png;base64,AAAA"},
	}}}
	textBody, err := json.Marshal(c.buildRequest(textOnly))
	if err != nil {
		t.Fatalf("marshal text request: %v", err)
	}
	imageBody, err := json.Marshal(c.buildRequest(withImage))
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	if !bytes.Equal(imageBody, textBody) {
		t.Fatalf("explicit official DeepSeek image request changed provider-visible bytes:\ntext:  %s\nimage: %s", textBody, imageBody)
	}
}

func TestOfficialDeepSeekDoesNotInjectToolResultImages(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-v4-pro",
		Extra: map[string]any{
			"vision":                true,
			"vision_model_explicit": true,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plainMessages := []provider.Message{
		{Role: provider.RoleUser, Content: "take a screenshot"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "shot", Arguments: "{}",
		}}},
		{
			Role: provider.RoleTool, ToolCallID: "c1", Name: "shot",
			Content: "[image: image/png]",
		},
	}
	imageMessages := append([]provider.Message(nil), plainMessages...)
	imageMessages[2].Images = []string{"data:image/png;base64,AAAA"}
	req := p.(*client).buildRequest(provider.Request{Messages: imageMessages})
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 without an injected image message", len(req.Messages))
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(body), "image_url") || strings.Contains(string(body), "base64,AAAA") {
		t.Fatalf("official DeepSeek request leaked tool image payload: %s", body)
	}
	plainBody, err := json.Marshal(p.(*client).buildRequest(provider.Request{Messages: plainMessages}))
	if err != nil {
		t.Fatalf("marshal plain request: %v", err)
	}
	if !bytes.Equal(body, plainBody) {
		t.Fatalf("official DeepSeek tool image metadata changed provider-visible bytes:\nplain: %s\nimage: %s", plainBody, body)
	}
}

func TestOfficialDeepSeekVisionSKUEmbedsUserImages(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   OfficialDeepSeekVisionModel,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.vision {
		t.Fatal("pinned official DeepSeek vision SKU must enable user image serialization")
	}
	req := c.buildRequest(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe",
		Images: []string{"data:image/png;base64,AAAA"},
	}}})
	parts, ok := req.Messages[0].Content.([]chatContentPart)
	if !ok || len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("vision SKU user content = %#v, want [text, image_url]", req.Messages[0].Content)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url = %+v", parts[1].ImageURL)
	}

	textOnly := c.buildRequest(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "hello",
	}}})
	if s, ok := textOnly.Messages[0].Content.(string); !ok || s != "hello" {
		t.Fatalf("vision SKU text-only content = %#v, want a string", textOnly.Messages[0].Content)
	}
}

func TestOfficialRequestURLImageHardLimit(t *testing.T) {
	p, err := New(provider.Config{BaseURL: "https://relay.test", Model: "deepseek-v4-flash", Extra: map[string]any{"request_url": "https://api.deepseek.com/v1/chat/completions", "vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(p.(*client).buildRequest(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}}))
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("official request URL leaked image: %s %v", body, err)
	}
}

func TestOfficialVisionExplicitOffRespectsResolvedMetadata(t *testing.T) {
	p, err := New(provider.Config{BaseURL: "https://api.deepseek.com", Model: OfficialDeepSeekVisionModel, Extra: map[string]any{"vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(p.(*client).buildRequest(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}}))
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("explicit off leaked image: %s %v", body, err)
	}
	if p.(provider.ModelInfoProvider).ModelInfo().SupportsInput(provider.ModalityImage) {
		t.Fatal("metadata disagrees with serializer")
	}
}

func TestOfficialDeepSeekVisionSKUOmitsToolImages(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   OfficialDeepSeekVisionModel,
		Extra:   map[string]any{"vision": true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plain := []provider.Message{
		{Role: provider.RoleUser, Content: "take a screenshot"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "shot", Arguments: "{}",
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]"},
	}
	withToolImage := append([]provider.Message(nil), plain...)
	withToolImage[2].Images = []string{"data:image/png;base64,AAAA"}
	c := p.(*client)
	body, err := json.Marshal(c.buildRequest(provider.Request{Messages: withToolImage}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "base64,AAAA") {
		t.Fatalf("official DeepSeek vision SKU leaked tool image payload: %s", body)
	}
	plainBody, err := json.Marshal(c.buildRequest(provider.Request{Messages: plain}))
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	if !bytes.Equal(body, plainBody) {
		t.Fatalf("tool images changed official DeepSeek vision SKU bytes:\nplain: %s\nimage: %s", plainBody, body)
	}
}

func TestCustomDeepSeekProtocolGatewayPreservesExplicitVision(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "deepseek-gateway",
		BaseURL: "https://gateway.example/v1",
		Model:   "deepseek-v4-pro",
		Extra: map[string]any{
			"reasoning_protocol": "deepseek",
			"vision":             true,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if !c.deepseek || !c.vision {
		t.Fatalf("deepseek=%v vision=%v, want both enabled", c.deepseek, c.vision)
	}
	req := c.buildRequest(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "describe",
		Images: []string{"data:image/png;base64,AAAA"},
	}}})
	parts, ok := req.Messages[0].Content.([]chatContentPart)
	if !ok || len(parts) != 2 || parts[1].ImageURL == nil {
		t.Fatalf("custom gateway content = %#v, want [text, image_url]", req.Messages[0].Content)
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
