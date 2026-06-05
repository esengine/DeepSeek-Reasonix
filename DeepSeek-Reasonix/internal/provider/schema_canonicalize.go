package provider

import (
	"encoding/json"
	"sort"
)

// CanonicalizeSchema recursively stabilizes a JSON Schema so the same logical
// schema always produces the same byte representation.
func CanonicalizeSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
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

var setLikeSchemaArrays = map[string]bool{
	"required":          true,
	"dependentRequired": true,
}

func canonicalizeSchemaValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		for k, inner := range val {
			val[k] = canonicalizeSchemaValue(inner)
		}
		for key := range val {
			if setLikeSchemaArrays[key] {
				if arr, ok := val[key].([]any); ok {
					sort.SliceStable(arr, func(i, j int) bool {
						return schemaJSONString(arr[i]) < schemaJSONString(arr[j])
					})
				}
			}
		}
		if dr, ok := val["dependentRequired"]; ok {
			if drMap, ok := dr.(map[string]any); ok {
				for _, inner := range drMap {
					if arr, ok := inner.([]any); ok {
						sort.SliceStable(arr, func(i, j int) bool {
							return schemaJSONString(arr[i]) < schemaJSONString(arr[j])
						})
					}
				}
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

func schemaJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
