package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
)

// 1.x logs mark a message "origin":"host" or "user". Reading one must not fail
// on the host's own turn origin, which is stored under another name.
func TestA1xOriginStringStillDecodes(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hi","origin":"host"}`), &m); err != nil {
		t.Fatalf("a 1.x message no longer decodes: %v", err)
	}
	if m.Origin != nil {
		t.Fatalf("1.x's host/user marker was read as a turn origin: %+v", m.Origin)
	}
	in := Message{Role: RoleUser, Content: "x", Origin: &Origin{Kind: "message", Agent: "a-1"}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Message
	if err := json.Unmarshal(data, &out); err != nil || out.Origin == nil || out.Origin.Agent != "a-1" {
		t.Fatalf("origin did not round-trip: %s → %+v, %v", data, out.Origin, err)
	}
}

// A secret the host holds for the person never reaches a model in any message
// it rides in, and the conversation the host keeps is left as it was.
func TestARegisteredSecretIsMaskedAtTheModelBoundary(t *testing.T) {
	secrets.RegisterValue("SHOP_KEY", "sk-shop-0123456789")
	defer secrets.UnregisterValue("sk-shop-0123456789")
	in := []Message{
		{Role: RoleUser, Content: "my key is sk-shop-0123456789"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"echo sk-shop-0123456789"}`}}},
	}
	out := ModelMessages(in)
	if strings.Contains(out[0].Content, "sk-shop") || strings.Contains(out[1].ToolCalls[0].Arguments, "sk-shop") || !strings.Contains(out[0].Content, "[secret:SHOP_KEY]") {
		t.Fatalf("model messages = %+v", out)
	}
	if !strings.Contains(in[0].Content, "sk-shop-0123456789") || !strings.Contains(in[1].ToolCalls[0].Arguments, "sk-shop-0123456789") {
		t.Fatal("masking rewrote the host's own copy")
	}
}
