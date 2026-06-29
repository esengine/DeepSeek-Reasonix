package plugin

import "testing"

// remoteTool and lazyTool report PlanModeUntrustedReadOnly()==true only when
// ReadOnly() is true *and* the read-only flag did not come from a first-party
// Spec.ReadOnlyToolNames override — i.e. it came from the server's untrusted
// readOnlyHint. Plan mode uses this to refuse such tools by default while still
// trusting first-party overrides (e.g. codeGraph) and built-ins.
func TestPluginToolsPlanModeUntrustedReadOnly(t *testing.T) {
	cases := []struct {
		name            string
		readOnly        bool
		readOnlyTrusted bool
		want            bool
	}{
		{"readOnlyHint only is untrusted", true, false, true},
		{"first-party override is trusted", true, true, false},
		{"not read-only is not untrusted", false, false, false},
		{"not read-only even if trusted-flagged", false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := &remoteTool{readOnly: c.readOnly, readOnlyTrusted: c.readOnlyTrusted}
			if got := rt.PlanModeUntrustedReadOnly(); got != c.want {
				t.Errorf("remoteTool.PlanModeUntrustedReadOnly() = %v, want %v", got, c.want)
			}
			lt := &lazyTool{readOnly: c.readOnly, readOnlyTrusted: c.readOnlyTrusted}
			if got := lt.PlanModeUntrustedReadOnly(); got != c.want {
				t.Errorf("lazyTool.PlanModeUntrustedReadOnly() = %v, want %v", got, c.want)
			}
		})
	}
}
