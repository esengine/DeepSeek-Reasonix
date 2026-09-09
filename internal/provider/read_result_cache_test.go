package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestReadResultEnvelopeDoesNotAffectProviderVisibleBytes proves that the
// host-only read delivery envelope attached to a reader result leaves
// ModelMessages — and therefore provider-visible conversation bytes — unchanged.
func TestReadResultEnvelopeDoesNotAffectProviderVisibleBytes(t *testing.T) {
	base := []Message{
		{Role: RoleUser, Content: "look at main.go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"main.go"}`}}},
		{Role: RoleTool, ToolCallID: "c1", Name: "read_file", Content: "   1→package main\n"},
	}
	withMeta := append([]Message(nil), base...)
	withMeta[2].ReadResult = json.RawMessage(`{"protocol_version":1,"source":{"canonical_path":"/w/main.go","version_token":"rw1:abc"},"intent":"inspect","delivered_ranges":[{"start":0,"end":1}],"eof":true}`)

	visBase, err := json.Marshal(ModelMessages(base))
	if err != nil {
		t.Fatal(err)
	}
	visMeta, err := json.Marshal(ModelMessages(withMeta))
	if err != nil {
		t.Fatal(err)
	}
	if string(visBase) != string(visMeta) {
		t.Fatalf("provider-visible messages diverged after local read metadata\nbase: %s\nmeta: %s", visBase, visMeta)
	}
	if strings.Contains(string(visMeta), "read_result") || strings.Contains(string(visMeta), "version_token") {
		t.Fatalf("local read envelope leaked into provider-visible JSON: %s", visMeta)
	}
}
