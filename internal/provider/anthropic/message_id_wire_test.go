package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

// Message ids are local transcript identity and must never change the
// Messages API request bytes (cache_control prefixes depend on them).
func TestMessageIDNeverReachesMessagesWire(t *testing.T) {
	plain := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "list the files"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "ls", Arguments: `{"path":"."}`}}},
		{Role: provider.RoleTool, Content: "main.go", ToolCallID: "call_1", Name: "ls"},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	tagged := append([]provider.Message(nil), plain...)
	for i := range tagged {
		tagged[i].ID = "01ARZ3NDEKTSV4RRFFQ69G5FA" + string(rune('A'+i))
	}
	c := &client{model: "claude-opus-4-8"}
	want, err := json.Marshal(c.buildRequest(context.Background(), provider.Request{Messages: plain}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(c.buildRequest(context.Background(), provider.Request{Messages: tagged}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("message ids changed request bytes\nplain=%s\ntagged=%s", want, got)
	}
	if bytes.Contains(got, []byte("01ARZ3NDEKTSV4RRFFQ69G5FA")) {
		t.Fatalf("message id leaked into request: %s", got)
	}
}
