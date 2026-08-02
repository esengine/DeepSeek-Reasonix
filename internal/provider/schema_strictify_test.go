package provider

import (
	"encoding/json"
	"testing"
)

func TestStrictifySchema(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "minLength": 1, "maxLength": 200},
			"count": {"type": "integer"},
			"tags": {"type": "array", "items": {"type": "string"}, "minItems": 1, "maxItems": 5}
		},
		"required": ["path"]
	}`)
	got := StrictifySchema(raw)
	var v map[string]any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("strictified schema is not valid JSON: %v\n%s", err, got)
	}
	// All properties become required, sorted.
	req, _ := v["required"].([]any)
	if len(req) != 3 || req[0] != "count" || req[1] != "path" || req[2] != "tags" {
		t.Fatalf("required = %v, want sorted [count path tags]", req)
	}
	if v["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", v["additionalProperties"])
	}
	props, _ := v["properties"].(map[string]any)
	pathSchema, _ := props["path"].(map[string]any)
	if _, ok := pathSchema["minLength"]; ok {
		t.Fatalf("minLength should be removed under strict mode: %v", pathSchema)
	}
	if _, ok := pathSchema["maxLength"]; ok {
		t.Fatalf("maxLength should be removed under strict mode: %v", pathSchema)
	}
	tagsSchema, _ := props["tags"].(map[string]any)
	if _, ok := tagsSchema["minItems"]; ok {
		t.Fatalf("minItems should be removed under strict mode: %v", tagsSchema)
	}
	// Nested items are recursed into and keep their own object contract.
	items, _ := tagsSchema["items"].(map[string]any)
	if items["type"] != "string" {
		t.Fatalf("items schema mangled: %v", items)
	}

	// Unparseable input passes through unchanged.
	if got := StrictifySchema(json.RawMessage("not json")); string(got) != "not json" {
		t.Fatalf("unparseable schema changed: %q", got)
	}
}

func TestStrictifySchemaNestedObject(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"opts": {
				"type": "object",
				"properties": {"a": {"type": "string"}},
				"required": ["a"]
			}
		}
	}`)
	got := StrictifySchema(raw)
	var v map[string]any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("strictified schema is not valid JSON: %v", err)
	}
	props, _ := v["properties"].(map[string]any)
	opts, _ := props["opts"].(map[string]any)
	if opts["additionalProperties"] != false {
		t.Fatalf("nested object additionalProperties = %v, want false", opts["additionalProperties"])
	}
	nestedProps, _ := opts["properties"].(map[string]any)
	if _, ok := nestedProps["a"]; !ok {
		t.Fatalf("nested properties lost: %v", opts)
	}
}
