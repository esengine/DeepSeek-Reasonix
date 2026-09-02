package responses

import (
	"testing"

	"reasonix/internal/provider"
)

func TestNativeToolSearchRequiresFirstPartyResponsesAndKnownModel(t *testing.T) {
	firstParty := New(Config{Name: "renamed-provider", BaseURL: "https://api.openai.com", Model: "gpt-5.5"}).(*client)
	if !firstParty.NativeToolSearchAvailable() {
		t.Fatal("first-party Responses client with a supported model should advertise tool search")
	}
	gateway := New(Config{Name: "openai", BaseURL: "https://gateway.example", Model: "gpt-5.5"}).(*client)
	if gateway.NativeToolSearchAvailable() {
		t.Fatal("provider name must not enable tool search on a compatible gateway")
	}
	oldModel := New(Config{Name: "openai", BaseURL: "https://api.openai.com", Model: "gpt-5.2"}).(*client)
	if oldModel.NativeToolSearchAvailable() {
		t.Fatal("unknown model capability must fail closed")
	}
}

func TestResponsesToolSearchWireIncludesSearchAndDeferredFunction(t *testing.T) {
	c := New(Config{Name: "openai", BaseURL: "https://api.openai.com", Model: "gpt-5.5"}).(*client)
	tools := encodeResponsesTools(c, provider.Request{
		ToolSearch: &provider.ToolSearch{Enabled: true},
		Tools:      []provider.ToolSchema{{Name: "mcp__svc__search", Parameters: []byte(`{"type":"object"}`), Deferred: true}},
	})
	if len(tools) != 2 || tools[0]["type"] != "tool_search" || tools[1]["defer_loading"] != true {
		t.Fatalf("encoded tools = %#v", tools)
	}
	if _, strict := tools[1]["strict"]; strict {
		t.Fatal("third-party MCP schemas must rely on host validation unless explicitly strict-compatible")
	}
}
