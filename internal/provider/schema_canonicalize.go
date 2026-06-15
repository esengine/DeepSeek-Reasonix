package provider

import (
	"encoding/json"
	"sort"
)

// CanonicalizeSchema recursively stabilizes a JSON Schema so the same logical
// schema always produces the same byte representation.
func CanonicalizeSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		// A tool with no parameters (common for MCP tools) yields an empty
		// schema. An empty json.RawMessage makes json.Marshal of the enclosing
		// request fail ("unexpected end of JSON input") and bricks the whole
		// provider; emit a valid empty-object schema instead.
		return json.RawMessage(`{"type":"object"}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	canon := canonicalizeSchemaValue(v)
	b, err := json.Marshal(canon)
	if err != nil {
		return raw
	}
	return json.RawMessage(b)
}

func canonicalizeSchemaValue(v any) any {
	return canonicalizeSchemaObject(v)
}

func canonicalizeSchemaObject(v any) any {
	switch val := v.(type) {
	case map[string]any:
		for k, inner := range val {
			switch k {
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
				val[k] = canonicalizeNamedSchemas(inner)
			case "dependentRequired":
				val[k] = canonicalizeDependentRequired(inner)
			default:
				val[k] = canonicalizeSchemaObject(inner)
			}
		}
		if req, ok := val["required"]; ok {
			if arr, ok := req.([]any); ok {
				sortSchemaArray(arr)
			} else {
				// Some MCP servers emit OpenAPI-style property metadata such as
				// {"required": true}. OpenAI-compatible function schemas require
				// JSON Schema's array form; dropping the invalid value keeps the
				// whole tool list from being rejected with HTTP 400.
				delete(val, "required")
			}
		}
		if dr, ok := val["dependentRequired"]; ok && !isJSONObject(dr) {
			delete(val, "dependentRequired")
		}
		return val
	case []any:
		for i, elem := range val {
			val[i] = canonicalizeSchemaObject(elem)
		}
		return val
	default:
		return v
	}
}

func canonicalizeNamedSchemas(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return canonicalizeSchemaObject(v)
	}
	for name, schema := range m {
		m[name] = canonicalizeSchemaObject(schema)
	}
	return m
}

func canonicalizeDependentRequired(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for key, inner := range m {
		if arr, ok := inner.([]any); ok {
			sortSchemaArray(arr)
		} else {
			delete(m, key)
		}
	}
	return m
}

func isJSONObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func sortSchemaArray(arr []any) {
	sort.SliceStable(arr, func(i, j int) bool {
		return schemaJSONString(arr[i]) < schemaJSONString(arr[j])
	})
}

func schemaJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
