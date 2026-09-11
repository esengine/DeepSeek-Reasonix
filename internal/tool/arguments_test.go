package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type schemaTool struct {
	name   string
	schema json.RawMessage
}

func (s schemaTool) Name() string                                           { return s.name }
func (s schemaTool) Description() string                                    { return "" }
func (s schemaTool) Schema() json.RawMessage                                { return s.schema }
func (schemaTool) ReadOnly() bool                                           { return true }
func (schemaTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

type stubMCPTool struct{ schemaTool }

func (stubMCPTool) MCPServerName() string  { return "srv" }
func (stubMCPTool) MCPRawToolName() string { return "tool" }

func TestValidateArgumentsDraftsAndEnums(t *testing.T) {
	schema2020 := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"mode":{"type":"string","enum":["a","b"]}},"required":["mode"],"additionalProperties":false}`)
	target := schemaTool{name: "t", schema: schema2020}
	if got := ValidateArguments(target, json.RawMessage(`{"mode":"a"}`)); len(got.Violations) != 0 || got.CompileErr != nil {
		t.Fatalf("valid args: %+v", got)
	}
	got := ValidateArguments(target, json.RawMessage(`{"mode":"z"}`))
	if len(got.Violations) == 0 {
		t.Fatal("enum mismatch accepted")
	}
	if got.Violations[0].Keyword != "enum" || strings.Contains(got.Violations[0].Expected, "z") {
		t.Fatalf("violation leaked value or missed enum: %+v", got.Violations[0])
	}
}

func TestValidateArgumentsDefaultDraft2020AndDraft7Fallback(t *testing.T) {
	explicit := json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"n":{"type":"integer","minimum":1}},"required":["n"]}`)
	got := ValidateArguments(schemaTool{name: "d7", schema: explicit}, json.RawMessage(`{"n":2}`))
	if got.CompileErr != nil || len(got.Violations) != 0 {
		t.Fatalf("explicit draft-07: %+v", got)
	}
	implicit := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`)
	got = ValidateArguments(schemaTool{name: "d2020", schema: implicit}, json.RawMessage(`{"n":2}`))
	if got.CompileErr != nil {
		t.Fatalf("implicit 2020-12 compile: %v", got.CompileErr)
	}
}

func TestValidateArgumentsRejectsExternalRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(path, []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fileURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"` + fileURL + `"}}}`)
	got := ValidateArguments(schemaTool{name: "builtin", schema: raw}, json.RawMessage(`{"x":"a"}`))
	if got.CompileErr == nil {
		t.Fatal("built-in schema with file $ref must be a compile error")
	}
	skipped := ValidateArguments(stubMCPTool{schemaTool{name: "mcp", schema: raw}}, json.RawMessage(`{"x":"a"}`))
	if !skipped.Skipped {
		t.Fatalf("third-party MCP uncompilable schema must skip, got %+v", skipped)
	}
}

func TestInvalidateArgumentSchemasDropsCompiledValidator(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
	target := schemaTool{name: "t", schema: schema}
	if got := ValidateArguments(target, json.RawMessage(`{"n":1}`)); got.CompileErr != nil {
		t.Fatalf("compile: %v", got.CompileErr)
	}
	fp := SchemaFingerprint(schema)
	if _, ok := argumentSchemaCache.Load(fp); !ok {
		t.Fatal("expected compiled validator cache entry")
	}
	InvalidateArgumentSchemas([]string{fp})
	if _, ok := argumentSchemaCache.Load(fp); ok {
		t.Fatal("invalidated fingerprint still cached")
	}
}

func TestValidateArgumentsNullBecomesObject(t *testing.T) {
	target := schemaTool{name: "t", schema: json.RawMessage(`{"type":"object"}`)}
	if got := ValidateArguments(target, nil); len(got.Violations) != 0 {
		t.Fatalf("nil args: %+v", got)
	}
	if got := ValidateArguments(target, json.RawMessage(`null`)); len(got.Violations) != 0 {
		t.Fatalf("null args: %+v", got)
	}
}

func TestValidateArgumentsCapsViolations(t *testing.T) {
	props := map[string]any{}
	required := make([]string, 0, 12)
	for i := range 12 {
		name := "f" + string(rune('a'+i))
		props[name] = map[string]any{"type": "string"}
		required = append(required, name)
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
	got := ValidateArguments(schemaTool{name: "t", schema: schema}, json.RawMessage(`{}`))
	if len(got.Violations) > maxArgumentViolations {
		t.Fatalf("violations = %d, want <= %d", len(got.Violations), maxArgumentViolations)
	}
}

func TestSchemaFingerprintStable(t *testing.T) {
	raw := json.RawMessage(`{"type":"object"}`)
	copyRaw := append(json.RawMessage(nil), raw...)
	if SchemaFingerprint(raw) != SchemaFingerprint(copyRaw) || SchemaFingerprint(raw) == "" {
		t.Fatal("fingerprint not stable")
	}
}
