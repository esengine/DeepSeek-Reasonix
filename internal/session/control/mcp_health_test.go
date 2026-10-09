package control

import (
	"errors"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/plugin"
)

func TestConfiguredMCPStatusPreservesCallableStandby(t *testing.T) {
	for _, tc := range []struct {
		state MCPServerState
		tools int
		want  string
	}{
		{MCPServerState{Enabled: true}, 12, "standby"},
		{MCPServerState{Enabled: true}, 0, "idle"},
		{MCPServerState{}, 12, "disabled"},
		{MCPServerState{Pending: true}, 0, "pending"},
	} {
		if got := configuredMCPStatus(tc.state, tc.tools); got != tc.want {
			t.Errorf("status(%+v, %d) = %q, want %q", tc.state, tc.tools, got, tc.want)
		}
	}
}

func TestMCPServerHealthIncludesSessionOnlyFailure(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := plugin.NewHost()
	c := New(Options{Host: host, WorkspaceRoot: testenv.TempDir(t)})
	defer c.Close()
	host.RecordFailure(plugin.Spec{Name: "editor-tools", Type: "http"}, errors.New("connection refused"))
	rows := c.MCPServerHealth()
	if len(rows) != 1 || rows[0].Name != "editor-tools" || rows[0].Status != "failed" || rows[0].Error == "" {
		t.Fatalf("session-only server health = %+v", rows)
	}
}

func TestMCPServerHealthReportsLaunchApprovalAsPending(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := plugin.NewHost()
	c := New(Options{Host: host, WorkspaceRoot: testenv.TempDir(t)})
	defer c.Close()
	host.RecordLaunchApprovalRequired(plugin.Spec{Name: "project-tools", Type: "stdio"})
	rows := c.MCPServerHealth()
	if len(rows) != 1 || rows[0].Name != "project-tools" || rows[0].Status != "pending" || rows[0].Error == "" {
		t.Fatalf("launch approval health = %+v", rows)
	}
	host.RecordFailure(plugin.Spec{Name: "project-tools", Type: "stdio"}, errors.New("process exited"))
	rows = c.MCPServerHealth()
	if len(rows) != 1 || rows[0].Status != "failed" {
		t.Fatalf("a real connection failure was hidden as pending: %+v", rows)
	}
}
