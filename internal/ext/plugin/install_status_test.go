package plugin

import (
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/base/secrets"
)

func TestInstallResultClassifiesStatusByIdentity(t *testing.T) {
	for _, tc := range []struct {
		status int
		state  string
		action string
	}{
		{401, "action_required", "authenticate"},
		{403, "action_required", "authenticate"},
		{500, "issue", "retry"},
	} {
		err := secrets.DiagnosticError(fmt.Errorf("MCP startup initialize failed: %w", &httpStatusError{Status: tc.status, BodyBytes: 12}))
		if !errors.As(err, new(*httpStatusError)) {
			t.Fatalf("identity lost through the diagnostic wrapper: %v", err)
		}
		got := InstallResultForError("docs", err)
		if got.State != tc.state || got.Action != tc.action {
			t.Errorf("status %d: state=%q action=%q, want %q %q", tc.status, got.State, got.Action, tc.state, tc.action)
		}
	}
}
