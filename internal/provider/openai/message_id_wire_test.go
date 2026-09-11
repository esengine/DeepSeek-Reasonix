package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

// Message ids are local transcript identity. The chat wire struct copies
// fields explicitly, so a tagged history must serialize byte-for-byte like an
// untagged one; anything else would break the prompt-cache prefix.
func TestMessageIDNeverReachesChatWire(t *testing.T) {
	plain := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "list the files"},
		{Role: provider.RoleAssistant, ReasoningContent: "think", ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "ls", Arguments: `{"path":"."}`}}},
		{Role: provider.RoleTool, Content: "main.go", ToolCallID: "call_1", Name: "ls"},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	tagged := append([]provider.Message(nil), plain...)
	for i := range tagged {
		tagged[i].ID = "01ARZ3NDEKTSV4RRFFQ69G5FA" + string(rune('A'+i))
	}
	c := &client{model: "deepseek-v4"}
	want, err := json.Marshal(c.buildRequest(provider.Request{Messages: plain}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(c.buildRequest(provider.Request{Messages: tagged}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("message ids changed chat request bytes\nplain=%s\ntagged=%s", want, got)
	}
	if bytes.Contains(got, []byte("01ARZ3NDEKTSV4RRFFQ69G5FA")) {
		t.Fatalf("message id leaked into request: %s", got)
	}
}
