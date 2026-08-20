package config

import "testing"

func TestAddPermissionRuleSaveTimeValidation(t *testing.T) {
	c := Default()
	// Structural typos are rejected before the rule is installed.
	for _, bad := range []string{"Edit()", "Bash=", "Edit(src", "()"} {
		if err := c.AddPermissionRule("allow", bad); err == nil {
			t.Errorf("AddPermissionRule(%q): expected error", bad)
		}
		if len(c.Permissions.Allow) != 0 {
			t.Fatalf("rule %q was installed despite validation failure", bad)
		}
	}
	// Unknown tool and out-of-workspace findings are warnings, so MCP/session
	// tool rules still save; diagnostics surface the warning instead.
	for _, ok := range []string{"edit_file(src/**)", "Edit(src/**)", "Bash(go test:*)", "mcp_tool:custom"} {
		if err := c.AddPermissionRule("allow", ok); err != nil {
			t.Errorf("AddPermissionRule(%q): unexpected error %v", ok, err)
		}
	}
	if len(c.Permissions.Allow) != 4 {
		t.Fatalf("allow list = %v, want four accepted rules", c.Permissions.Allow)
	}
}
