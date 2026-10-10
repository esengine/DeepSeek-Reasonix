package chartspec

import "encoding/json"

// ToolName is the name a frontend recognises a chart call by.
const ToolName = "render_chart"

const proxyName = "use_capability"

// FromCall returns the spec a render_chart call carried: either the tool called
// directly, or reached through the capability proxy with action "call". A lookup
// of the tool, a call to anything else and a spec the validator refuses are all
// not charts. Whether the call settled or was refused is the caller's to check.
func FromCall(name, resolvedName, args string) (*Spec, bool) {
	raw := json.RawMessage(args)
	switch {
	case name == ToolName:
	case name == proxyName && resolvedName == ToolName:
		var env struct {
			Action    string          `json:"action"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(raw, &env) != nil || env.Action != "call" {
			return nil, false
		}
		raw = env.Arguments
	default:
		return nil, false
	}
	spec, err := Parse(raw)
	return spec, err == nil
}
