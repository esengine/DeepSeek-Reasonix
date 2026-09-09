package responses

import (
	"bytes"
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

// Message ids are local transcript identity. Both the request body and the
// stateful-continuation digest must ignore them, or a reload would force a
// full replay and lose previous_response_id reuse.
func TestMessageIDNeverReachesResponsesWire(t *testing.T) {
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
	c := New(Config{Name: "responses", BaseURL: "https://example.com", Model: "model"}).(*client)
	plainBody, _, _ := c.buildRequestBody(provider.Request{Messages: plain})
	taggedBody, _, _ := c.buildRequestBody(provider.Request{Messages: tagged})
	want, err := json.Marshal(plainBody)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(taggedBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("message ids changed request bytes\nplain=%s\ntagged=%s", want, got)
	}
	if bytes.Contains(got, []byte("01ARZ3NDEKTSV4RRFFQ69G5FA")) {
		t.Fatalf("message id leaked into request: %s", got)
	}
	if c.conversationDigest(plain) != c.conversationDigest(tagged) {
		t.Fatal("conversationDigest must ignore message ids")
	}
}
