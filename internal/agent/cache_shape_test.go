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

func TestCompareShapeIgnoresBareLogRewriteVersionDrift(t *testing.T) {
	before := CaptureShape("system", nil, 5)
	// Simulates AddDecisionReceipt/UpdateToolCallPreview/UpdateToolCallResolution/
	// ReplaceLocalMetadata bumping rewriteVersion alone, with no drained content
	// reason: none of those touch provider-visible bytes, so this must not be
	// reported as a cache-prefix change.
	after := CaptureShape("system", nil, 9)
	if diag := CompareShape(before, after, nil, nil); diag.PrefixChanged {
		t.Fatalf("LogRewriteVersion drift with no drained reason must not report a change: %+v", diag)
	}

	if diag := CompareShape(before, after, nil, []string{"compact_auto"}); !diag.PrefixChanged || len(diag.PrefixChangeReasons) != 1 || diag.PrefixChangeReasons[0] != "compact_auto" {
		t.Fatalf("a real drained reason must be reported: %+v", diag)
	}
}
