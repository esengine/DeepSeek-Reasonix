package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
)

func TestOutboundMasksToolDefinitionsAndEscapedArguments(t *testing.T) {
	const value = "schema-secret-0123456789"
	secretstest.Hold("SCHEMA_BOUNDARY", value)
	req := Request{
		Tools:    []ToolSchema{{Name: "fixture", Description: "Uses " + value, Parameters: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","default":"schema-secret-0123456789"}}}`)}},
		Messages: []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "fixture", Arguments: `{"key":"\u0073chema-secret-0123456789"}`}}}},
	}
	out := MaskOutbound(req, Signed{})
	args := out.Messages[0].ToolCalls[0].Arguments
	var decoded any
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		t.Fatal(err)
	}
	normalized, _ := json.Marshal(decoded)
	if strings.Contains(string(normalized), value) || strings.Contains(out.Tools[0].Description, value) || strings.Contains(string(out.Tools[0].Parameters), value) {
		t.Fatalf("outbound leaked a secret in tool definitions or arguments: %+v, %s", out.Tools, args)
	}
	if req.Tools[0].Description != "Uses "+value || !strings.Contains(string(req.Tools[0].Parameters), value) {
		t.Fatal("masking changed the canonical tool definitions")
	}
	first, _ := json.Marshal(out)
	next, _ := json.Marshal(MaskOutbound(req, Signed{}))
	if string(first) != string(next) {
		t.Fatal("identical outbound requests changed cache bytes")
	}
}
