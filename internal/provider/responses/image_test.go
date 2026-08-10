package responses

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

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
