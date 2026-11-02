package providerconv

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/provider"
)

// Whatever crosses into an extension process — a hook's context, a provider
// request, tool calls, a plugin-hosted model's request — leaves masked,
// signed bytes included: the host cannot tell what the receiver verifies.
func TestEverythingCrossingIntoAnExtensionIsMasked(t *testing.T) {
	const value = "sk-extension-0123456789"
	secretstest.Hold("EXT_KEY", value)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "use " + value},
		{Role: provider.RoleAssistant, ReasoningContent: value, ReasoningSignature: "sig",
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"echo ` + value + `"}`, ThoughtSignature: "ts"}}},
	}
	crossings := map[string]any{
		"messages":   MessagesToProtocol(msgs),
		"request":    RequestToProtocol(provider.Request{Messages: msgs}),
		"tool calls": ToolCallsToProtocol(msgs[1].ToolCalls),
	}
	for name, wire := range crossings {
		data, _ := json.Marshal(wire)
		if strings.Contains(string(data), value) || !strings.Contains(string(data), "[secret:EXT_KEY]") {
			t.Errorf("%s crossed as %s", name, data)
		}
	}
	if !strings.Contains(msgs[0].Content, value) {
		t.Fatal("masking rewrote the host's own copy")
	}
}
