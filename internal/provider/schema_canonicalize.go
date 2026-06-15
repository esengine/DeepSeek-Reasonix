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
	switch val := v.(type) {
	case map[string]any:
		for k, inner := range val {
			val[k] = canonicalizeSchemaValue(inner)
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
		if dr, ok := val["dependentRequired"]; ok {
			if drMap, ok := dr.(map[string]any); ok {
				for key, inner := range drMap {
					if arr, ok := inner.([]any); ok {
						sortSchemaArray(arr)
					} else {
						delete(drMap, key)
					}
				}
			} else {
				delete(val, "dependentRequired")
			}
		}
		return val
	case []any:
		for i, elem := range val {
			val[i] = canonicalizeSchemaValue(elem)
		}
		return val
	default:
		return v
	}
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
