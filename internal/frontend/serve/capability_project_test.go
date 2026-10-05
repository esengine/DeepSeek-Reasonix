package serve

import (
	"testing"

	"reasonix/internal/session/control"
)

// A repository-declared server nobody answered for is off, but not because the
// user refused it. Reporting both as "disabled" is what makes a project's MCP
// read as quietly gone.
func TestPendingIsReportedApartFromDisabled(t *testing.T) {
	for _, c := range []struct {
		name  string
		state control.MCPServerState
		want  string
	}{
		{"awaiting a decision", control.MCPServerState{Pending: true}, "pending"},
		{"the user said no", control.MCPServerState{}, "disabled"},
		{"running", control.MCPServerState{Enabled: true}, "idle"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := mcpProjectState(c.state); got != c.want {
				t.Fatalf("state = %q, want %q", got, c.want)
			}
		})
	}
}
