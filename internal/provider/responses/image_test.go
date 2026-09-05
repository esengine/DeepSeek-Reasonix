package responses

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestOfficialDeepSeekVisionSKUEmbedsUserImages(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp",
	}).(*client)
	if !c.vision {
		t.Fatal("pinned official DeepSeek vision SKU must enable user image serialization")
	}
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "what is this",
		Images: []string{"data:image/png;base64,AAAA"},
	}}})
	items := body["input"].([]map[string]any)
	parts, ok := items[0]["content"].([]map[string]string)
	if !ok || len(parts) != 2 || parts[0]["type"] != "input_text" || parts[1]["type"] != "input_image" {
		t.Fatalf("vision SKU content = %#v, want input_text + input_image", items[0]["content"])
	}
	textOnly, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "hello",
	}}})
	if got, ok := textOnly["input"].([]map[string]any)[0]["content"].(string); !ok || got != "hello" {
		t.Fatalf("vision SKU text-only content = %#v, want a string", textOnly["input"].([]map[string]any)[0]["content"])
	}
}

func TestOfficialRequestURLImageHardLimit(t *testing.T) {
	c := New(Config{BaseURL: "https://relay.test", RequestURL: "https://api.deepseek.com/responses", Model: "deepseek-v4-flash", Extra: map[string]any{"vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}}).(*client)
	req, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}})
	body, err := json.Marshal(req)
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("official request URL leaked image: %s %v", body, err)
	}
}

func TestOfficialVisionExplicitOffRespectsResolvedMetadata(t *testing.T) {
	c := New(Config{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp", Extra: map[string]any{"vision": true}, ModelInfo: &provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText}}}).(*client)
	req, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}})
	body, err := json.Marshal(req)
	if err != nil || strings.Contains(string(body), "AAAA") {
		t.Fatalf("explicit off leaked image: %s %v", body, err)
	}
	if c.ModelInfo().SupportsInput(provider.ModalityImage) {
		t.Fatal("metadata disagrees with serializer")
	}
}

func TestModelInfoEnablesImageWireSerialization(t *testing.T) {
	c := New(Config{
		Name: "catalog", BaseURL: "https://example.test/v1", Model: "vision",
		ModelInfo: &provider.ModelInfo{ID: "vision", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}},
	}).(*client)
	if !c.vision {
		t.Fatal("model metadata should enable image wire serialization")
	}
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,AAAA"}}}})
	items := body["input"].([]map[string]any)
	parts := items[0]["content"].([]map[string]string)
	if len(parts) != 2 || parts[1]["type"] != "input_image" {
		t.Fatalf("content = %#v, want text + input_image parts", parts)
	}
}

func TestOfficialDeepSeekVisionSKUEmbedsURLAndFileID(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp",
	}).(*client)
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{{
		Role:    provider.RoleUser,
		Content: "what is this",
		Images: []string{
			"https://cdn.example.com/cat.png",
			"file-api-0a1b2c3d4e5f6071",
		},
	}}})
	items := body["input"].([]map[string]any)
	parts, ok := items[0]["content"].([]map[string]string)
	if !ok || len(parts) != 3 {
		t.Fatalf("content = %#v", items[0]["content"])
	}
	if parts[1]["type"] != "input_image" || parts[1]["image_url"] != "https://cdn.example.com/cat.png" {
		t.Fatalf("url part = %#v", parts[1])
	}
	if parts[2]["type"] != "input_image" || parts[2]["file_id"] != "file-api-0a1b2c3d4e5f6071" {
		t.Fatalf("file part = %#v", parts[2])
	}
}

func TestStatefulContinuationDoesNotDropUserImages(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp", Mode: "stateful",
	}).(*client)
	prefix := []provider.Message{
		{Role: provider.RoleSystem, Content: "You inspect images."},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "answer"},
	}
	c.lastResponseID = "resp_1"
	c.expectedPrefixDigest = c.conversationDigest(prefix)
	body, usedPrevious, _ := c.buildRequestBody(provider.Request{Messages: append(prefix, provider.Message{
		Role: provider.RoleUser, Content: "second", Images: []string{"data:image/png;base64,AAAA"},
	})})
	if usedPrevious {
		t.Fatal("stateful continuation must not use text-only previous_response_id fast path for image turns")
	}
	items, ok := body["input"].([]map[string]any)
	if !ok {
		t.Fatalf("input = %#v, want multimodal item list", body["input"])
	}
	found := false
	for _, item := range items {
		parts, ok := item["content"].([]map[string]string)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part["type"] == "input_image" && part["image_url"] == "data:image/png;base64,AAAA" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("stateful continuation input = %#v, missing user image", items)
	}
}

func TestOfficialDeepSeekVisionSKUOmitsToolImages(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp",
		Extra: map[string]any{"vision": true},
	}).(*client)
	plain := []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "vision result"},
	}
	withToolImage := append([]provider.Message(nil), plain...)
	withToolImage[2].Images = []string{"data:image/png;base64,VE9PTA=="}
	plainBody, err := json.Marshal(mustRequest(t, c, plain))
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	imageBody, err := json.Marshal(mustRequest(t, c, withToolImage))
	if err != nil {
		t.Fatalf("marshal image: %v", err)
	}
	if strings.Contains(string(imageBody), "VE9PTA") {
		t.Fatalf("official DeepSeek vision SKU leaked tool image payload: %s", imageBody)
	}
	if !bytes.Equal(imageBody, plainBody) {
		t.Fatalf("tool images changed official DeepSeek vision SKU bytes:\nplain: %s\nimage: %s", plainBody, imageBody)
	}
}

func mustRequest(t *testing.T, c *client, messages []provider.Message) map[string]any {
	t.Helper()
	body, _, _ := c.buildRequestBody(provider.Request{Messages: messages})
	return body
}

func TestOfficialDeepSeekResponsesImageMetadataMatchesTextOnlyWireBytes(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		Extra: map[string]any{"vision": true},
	}).(*client)
	plain := []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "vision result"},
	}
	withImages := append([]provider.Message(nil), plain...)
	withImages[0].Images = []string{"data:image/png;base64," + strings.Repeat("QUFB", 20_000)}
	withImages[2].Images = []string{"data:image/png;base64,VE9PTA=="}

	plainRequest, _, _ := c.buildRequestBody(provider.Request{Messages: plain})
	imageRequest, _, _ := c.buildRequestBody(provider.Request{Messages: withImages})
	plainBody, err := json.Marshal(plainRequest)
	if err != nil {
		t.Fatalf("marshal plain request: %v", err)
	}
	imageBody, err := json.Marshal(imageRequest)
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	if !bytes.Equal(imageBody, plainBody) {
		t.Fatalf("official DeepSeek Responses image metadata changed provider-visible bytes:\nplain: %s\nimage: %s", plainBody, imageBody)
	}
}
