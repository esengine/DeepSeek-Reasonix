package responses

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestOfficialDeepSeekResponsesImageMetadataMatchesTextOnlyWireBytes(t *testing.T) {
	c := New(Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro",
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

func TestToolResultImagesFollowTheRunOfOutputs(t *testing.T) {
	c := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5", Extra: map[string]any{"vision": true}}).(*client)
	body, _, _ := c.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}, {ID: "c2", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,AAAA"}},
		{Role: provider.RoleTool, ToolCallID: "c2", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,BBBB"}},
		{Role: provider.RoleAssistant, Content: "seen"},
	}})
	items := body["input"].([]map[string]any)
	var shape []string
	for _, item := range items {
		if typ, ok := item["type"].(string); ok {
			shape = append(shape, typ)
		} else {
			shape = append(shape, item["role"].(string))
		}
	}
	if got, want := strings.Join(shape, ","), "user,function_call,function_call,function_call_output,function_call_output,user,assistant"; got != want {
		t.Fatalf("input shape = %s, want %s", got, want)
	}
	parts, ok := items[5]["content"].([]map[string]string)
	if !ok || len(parts) != 3 {
		t.Fatalf("image turn content = %#v, want a lead and both images", items[5]["content"])
	}
	for i, want := range []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"} {
		if parts[i+1]["type"] != "input_image" || parts[i+1]["image_url"] != want {
			t.Fatalf("parts[%d] = %#v, want input_image %q", i+1, parts[i+1], want)
		}
	}

	plain := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5"}).(*client)
	plainBody, _, _ := plain.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "[image: image/png]", Images: []string{"data:image/png;base64,AAAA"}},
	}})
	if n := len(plainBody["input"].([]map[string]any)); n != 3 {
		t.Fatalf("a text-only model got %d input items, want no image turn", n)
	}
}
