package responses

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestCanSuppressWebSearchServerTool(t *testing.T) {
	c := New(Config{Name: "test", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash", WebSearch: true}).(*client)
	searchItem := json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"old"}}`)
	body, _, _ := c.buildRequestBody(provider.Request{
		DisableServerTools: true,
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Content: "prior answer", ResponsesItems: []json.RawMessage{searchItem}},
			{Role: provider.RoleUser, Content: "render only"},
		},
		Tools: []provider.ToolSchema{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 1 || tools[0]["type"] != "function" || tools[0]["name"] != "read_file" {
		t.Fatalf("tools = %#v, want client function only", body["tools"])
	}
	for _, item := range body["input"].([]map[string]any) {
		if item["type"] == "web_search_call" {
			t.Fatalf("input = %#v, want no server-search replay", body["input"])
		}
	}
}
