package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestCaptureShapeNormalizesToolSchemaOrder(t *testing.T) {
	schemas := []provider.ToolSchema{
		{
			Name:        "write_file",
			Description: "write a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}
	reordered := []provider.ToolSchema{schemas[1], schemas[0]}

	first := CaptureShape("system", schemas, 1)
	second := CaptureShape("system", reordered, 1)

	if first.ToolsHash != second.ToolsHash {
		t.Fatalf("ToolsHash should be stable across schema order: %q != %q", first.ToolsHash, second.ToolsHash)
	}
	if first.PrefixHash != second.PrefixHash {
		t.Fatalf("PrefixHash should be stable across schema order: %q != %q", first.PrefixHash, second.PrefixHash)
	}
	if schemas[0].Name != "write_file" || schemas[1].Name != "read_file" {
		t.Fatalf("CaptureShape mutated caller schema order: got [%s %s]", schemas[0].Name, schemas[1].Name)
	}
}
