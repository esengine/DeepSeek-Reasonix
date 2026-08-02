package provider

import (
	"encoding/json"
	"sort"
)

// StrictifySchema rewrites a tool parameters schema into the form strict
// function-calling mode requires (DeepSeek beta): every object's properties
// are all required with additionalProperties:false, and the subset keywords
// the strict validator rejects (string minLength/maxLength, array
// minItems/maxItems) are removed. Returns the input unchanged when it cannot
// be parsed as JSON.
func StrictifySchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	strictifySchemaValue(v)
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return json.RawMessage(b)
}

func strictifySchemaValue(v any) {
	switch val := v.(type) {
	case map[string]any:
		// The strict validator requires every object schema to list all
		// properties as required and forbid additional ones.
		if val["type"] == "object" {
			if props, ok := val["properties"].(map[string]any); ok {
				required := make([]string, 0, len(props))
				for name, sub := range props {
					required = append(required, name)
					strictifySchemaValue(sub)
				}
				sort.Strings(required)
				val["required"] = required
			}
			val["additionalProperties"] = false
		}
		// Subset keywords the strict validator rejects; keep the rest.
		for _, k := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
			delete(val, k)
		}
		// Recurse into nested schemas carried by any keyword.
		for k, inner := range val {
			switch k {
			case "properties", "patternProperties", "items", "prefixItems",
				"allOf", "anyOf", "oneOf", "not", "$defs", "definitions",
				"additionalProperties", "contains", "if", "then", "else":
				strictifySchemaValue(inner)
			}
		}
	case []any:
		for _, e := range val {
			strictifySchemaValue(e)
		}
	}
}
