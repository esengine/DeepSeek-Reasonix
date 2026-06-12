package codegraph

import (
	"strings"
	"testing"
)

// TestSteerTextNamesMatchRegisteredTools keeps the system-prompt steering names
// in lockstep with the daemon's tools. A model-visible name is
// "mcp__codegraph__" + the raw tool name with the spec's StripRawPrefix
// ("codegraph_") removed — the mirror of plugin.ToolPrefix + Spec.StripRawPrefix
// applied in boot.go. If a tool is added/renamed or the steering text drifts,
// this fails instead of silently telling the model to call a name it can't use.
func TestSteerTextNamesMatchRegisteredTools(t *testing.T) {
	const prefix = "mcp__codegraph__"

	want := make(map[string]bool, len(ReadOnlyToolNames()))
	for raw := range ReadOnlyToolNames() {
		want[prefix+strings.TrimPrefix(raw, "codegraph_")] = true
	}

	for name := range want {
		if !strings.Contains(SteerText, name) {
			t.Errorf("SteerText is missing %q — the model won't know this tool exists", name)
		}
	}

	for _, field := range strings.Fields(SteerText) {
		if strings.HasPrefix(field, prefix) && !want[field] {
			t.Errorf("SteerText names %q, which is not a registered codegraph tool", field)
		}
	}
}
