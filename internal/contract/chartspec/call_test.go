package chartspec

import (
	"encoding/json"
	"testing"
)

func capabilityArgs(t *testing.T, action string, spec string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"action": action, "capability_id": "tool:render_chart", "arguments": json.RawMessage(spec)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFromCall(t *testing.T) {
	cases := []struct {
		name, tool, resolved, args string
		want                       bool
	}{
		{"direct call", ToolName, "", okSpec, true},
		{"through the capability proxy", "use_capability", ToolName, capabilityArgs(t, "call", okSpec), true},
		{"proxy without a resolved name", "use_capability", "", capabilityArgs(t, "call", okSpec), false},
		{"a lookup is not a call", "use_capability", ToolName, capabilityArgs(t, "inspect", okSpec), false},
		{"a search is not a call", "use_capability", ToolName, capabilityArgs(t, "search", okSpec), false},
		{"another tool", "use_capability", "web_fetch", capabilityArgs(t, "call", okSpec), false},
		{"a spec the validator refuses", "use_capability", ToolName, capabilityArgs(t, "call", `{"spec_version":1,"title":"x","data":{"columns":[],"rows":[]},"marks":[]}`), false},
		{"malformed arguments", "use_capability", ToolName, `{not json`, false},
		{"empty arguments", ToolName, "", ``, false},
	}
	for _, c := range cases {
		spec, ok := FromCall(c.tool, c.resolved, c.args)
		if ok != c.want {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.want)
		}
		if ok && spec.Title != "Sales" {
			t.Errorf("%s: wrong spec %+v", c.name, spec)
		}
	}
}
