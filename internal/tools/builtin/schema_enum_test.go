package builtin

import (
	"encoding/json"
	"fmt"
	"testing"

	"reasonix/internal/contract/tool"
)

// Gemini's function-declaration validator accepts enum only on a string
// property with string members; anything else is a 400 for the whole request,
// so one tool's schema takes every other tool down with it.
func TestBuiltinSchemaEnumsAreStrings(t *testing.T) {
	for _, tl := range tool.Builtins() {
		var schema any
		if err := json.Unmarshal(tl.Schema(), &schema); err != nil {
			t.Fatalf("%s: schema is not JSON: %v", tl.Name(), err)
		}
		for _, bad := range nonStringEnums(schema, "$") {
			t.Errorf("%s: %s", tl.Name(), bad)
		}
	}
}

func nonStringEnums(node any, path string) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if enum, ok := n["enum"].([]any); ok {
			if n["type"] != "string" {
				out = append(out, fmt.Sprintf("%s: enum on type %v, want string", path, n["type"]))
			}
			for i, v := range enum {
				if _, ok := v.(string); !ok {
					out = append(out, fmt.Sprintf("%s.enum[%d]: %v is not a string", path, i, v))
				}
			}
		}
		for k, v := range n {
			out = append(out, nonStringEnums(v, path+"."+k)...)
		}
	case []any:
		for i, v := range n {
			out = append(out, nonStringEnums(v, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return out
}
