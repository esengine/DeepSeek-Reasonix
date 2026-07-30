package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestCanonicalizeSchemaStable(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name","age"]}`)
	first := canonicalizeSchema(schema)
	second := canonicalizeSchema(first)
	if string(first) != string(second) {
		t.Errorf("canonicalizeSchema is not idempotent:\n  first: %s\n  second: %s", first, second)
	}
}

func TestCanonicalizeSchemaSortsRequired(t *testing.T) {
	schema := json.RawMessage(`{"required":["c","a","b"],"type":"object"}`)
	result := canonicalizeSchema(schema)
	var m map[string]any
	json.Unmarshal(result, &m)
	arr := m["required"].([]any)
	if arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Errorf("required not sorted: %v", arr)
	}
}

func TestCanonicalizeSchemaPreservesEnum(t *testing.T) {
	schema := json.RawMessage(`{"enum":["c","a","b"]}`)
	result := canonicalizeSchema(schema)
	var m map[string]any
	json.Unmarshal(result, &m)
	arr := m["enum"].([]any)
	if arr[0] != "c" || arr[1] != "a" || arr[2] != "b" {
		t.Errorf("enum order was changed: %v", arr)
	}
}

func TestCanonicalizeSchemaSortsKeys(t *testing.T) {
	schema := json.RawMessage(`{"z":1,"a":2,"m":3,"type":"object","properties":{}}`)
	result := canonicalizeSchema(schema)
	// json.Marshal sorts map keys, so verify the JSON string directly.
	s := string(result)
	if s != `{"a":2,"m":3,"properties":{},"type":"object","z":1}` {
		t.Errorf("keys not sorted, got: %s", s)
	}
}

func TestCanonicalizeSchemaNested(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"inner":{"type":"object","required":["b","a"]}}}`)
	result := canonicalizeSchema(schema)
	var m map[string]any
	json.Unmarshal(result, &m)
	props := m["properties"].(map[string]any)
	inner := props["inner"].(map[string]any)
	req := inner["required"].([]any)
	if req[0] != "a" || req[1] != "b" {
		t.Errorf("nested required not sorted: %v", req)
	}
}

func TestCanonicalizeSchemaEquivalentOrderingMatches(t *testing.T) {
	first := canonicalizeSchema(json.RawMessage(`{"type":"object","required":["b","a"],"properties":{"b":{"description":"bee","type":"string"},"a":{"type":"integer"}}}`))
	second := canonicalizeSchema(json.RawMessage(`{"properties":{"a":{"type":"integer"},"b":{"type":"string","description":"bee"}},"required":["a","b"],"type":"object"}`))
	if string(first) != string(second) {
		t.Fatalf("equivalent schemas canonicalized differently:\n  first:  %s\n  second: %s", first, second)
	}
}

func TestRemoteToolSchemaCanonicalizesOnReturn(t *testing.T) {
	rt := &remoteTool{schema: json.RawMessage(`{"type":"object","required":["z","a"],"properties":{"z":{"type":"string"},"a":{"type":"string"}}}`)}
	if got, want := string(rt.Schema()), `{"properties":{"a":{"type":"string"},"z":{"type":"string"}},"required":["a","z"],"type":"object"}`; got != want {
		t.Fatalf("Schema() = %s, want %s", got, want)
	}
}

func TestSortToolsByName(t *testing.T) {
	tools := []tool.Tool{
		testTool{name: "zulu"},
		testTool{name: "alpha"},
		testTool{name: "mike"},
	}
	sorted := sortToolsByName(tools)
	if sorted[0].Name() != "alpha" || sorted[1].Name() != "mike" || sorted[2].Name() != "zulu" {
		t.Errorf("tools not sorted: %v", toolNames(sorted))
	}
	// Original should be unchanged
	if tools[0].Name() != "zulu" {
		t.Error("original slice was mutated")
	}
}

func TestNormalizeNameForToolNames(t *testing.T) {
	if got := normalizeName("valid_name-1"); got != "valid_name-1" {
		t.Fatalf("valid name changed: %q", got)
	}
	cases := []string{"@modelcontextprotocol/server-memory", "mcp server/fetch", "   "}
	for _, in := range cases {
		got := normalizeName(in)
		if got == "" || strings.ContainsAny(got, " @/") {
			t.Errorf("normalizeName(%q) = %q, want non-empty safe identifier", in, got)
		}
	}
}

func TestNormalizeNameAvoidsSanitizedCollisions(t *testing.T) {
	a := normalizeName("search/code")
	b := normalizeName("search_code")
	if a == b {
		t.Fatalf("normalized names collided: %q", a)
	}
	if b != "search_code" {
		t.Fatalf("valid identifier should stay stable, got %q", b)
	}
	if normalizeName("@foo") == normalizeName("foo") {
		t.Fatal("trimmed invalid prefix should not collapse onto valid name")
	}
}

func TestSummarizeFailureErrorSingleLine(t *testing.T) {
	got := summarizeFailureError(errors.New("npm error code ENOTEMPTY\nnpm error path /tmp/x"))
	if strings.Contains(got, "\n") || !strings.Contains(got, "ENOTEMPTY") {
		t.Fatalf("summary = %q", got)
	}
}

type testTool struct{ name string }

func (t testTool) Name() string                                                      { return t.name }
func (t testTool) Description() string                                               { return "" }
func (t testTool) Schema() json.RawMessage                                           { return nil }
func (t testTool) Execute(ctx context.Context, args json.RawMessage) (string, error) { return "", nil }
func (t testTool) ReadOnly() bool                                                    { return true }

func toolNames(ts []tool.Tool) []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name()
	}
	return names
}
