package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateToolSchemaAcceptsValidDrafts(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"values":{"type":"array","prefixItems":[{"type":"string"}]}}}`),
	} {
		if err := ValidateToolSchema(raw); err != nil {
			t.Fatalf("ValidateToolSchema(%s): %v", raw, err)
		}
	}
}

func TestValidateToolSchemaRejectsMalformedNestedArrayItems(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"options":{
				"type":"array",
				"items":{
					"key":{"description":"option key","type":"string"},
					"type":{"description":"option type","type":"string"},
					"value":{"description":"option value","type":"string"}
				}
			}
		}
	}`)
	err := ValidateToolSchema(raw)
	if err == nil {
		t.Fatal("malformed Yakit-style array item schema was accepted")
	}
	if msg := err.Error(); !strings.Contains(msg, "/properties/options/items/type") {
		t.Fatalf("error does not identify the malformed nested type: %v", err)
	}
}

func TestValidateToolSchemaRejectsNonObjectRootType(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"missing type":  json.RawMessage(`{}`),
		"string root":   json.RawMessage(`{"type":"string"}`),
		"nullable root": json.RawMessage(`{"type":["object","null"]}`),
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateToolSchema(raw)
			if err == nil {
				t.Fatalf("ValidateToolSchema(%s) returned nil", raw)
			}
			if !strings.Contains(err.Error(), `"object"`) {
				t.Fatalf("error does not name the object requirement: %v", err)
			}
		})
	}
}

func TestValidateToolSchemaRejectsFileRefsEvenWhenResolvable(t *testing.T) {
	// A resolvable local file proves rejection comes from the disabled loader,
	// not from the file happening to be missing or malformed.
	path := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(path, []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fileURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"` + fileURL + `"}}}`)
	if err := ValidateToolSchema(raw); err == nil {
		t.Fatalf("ValidateToolSchema resolved local file ref %s", fileURL)
	}
}

func TestValidateToolSchemaRejectsNonObjectRootAndExternalRefs(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"array root":   json.RawMessage(`[]`),
		"external ref": json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"https://example.com/schema.json"}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateToolSchema(raw); err == nil {
				t.Fatalf("ValidateToolSchema(%s) returned nil", raw)
			}
		})
	}
}
