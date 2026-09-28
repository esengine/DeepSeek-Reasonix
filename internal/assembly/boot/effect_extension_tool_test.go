package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A tool a plugin runtime serves reaches the model the way an MCP tool does:
// absent from the provider-visible list, so the cached prefix never moves, and
// callable through use_capability, where the call reaches the runtime and its
// answer comes back as the tool result.
func TestEffectAnExtensionToolIsCalledThroughTheCatalog(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "lexicon", map[string]any{
		"capabilities": []string{"tools"},
		"tools": []map[string]any{{
			"name": "lookup", "description": "Look a term up in the team glossary", "readOnly": true,
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"term": map[string]any{"type": "string"}}},
		}},
		"env": map[string]string{bootFakeEnvInitResult: `{"protocolVersion":"2","name":"lexicon","version":"1.0.0","stateSchemaVersion":0,"tools":["lookup"]}`},
	})
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("c1", "use_capability", map[string]any{
				"action": "call", "capability_id": "tool:ext__lexicon__lookup", "arguments": map[string]any{"term": "lease"},
			})
		},
	}}
	kind := "boot-exttool-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "what does lease mean here"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()
	reqs := agentRequests(rec.requests())
	if toolNames(reqs[0])["ext__lexicon__lookup"] {
		t.Fatal("the extension tool is in the provider-visible schema, where installing a plugin would move the cached prefix")
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], `looked up {"term":"lease"}`) {
		t.Fatalf("tool results = %q, want the runtime's answer to the model's arguments", results)
	}
}
