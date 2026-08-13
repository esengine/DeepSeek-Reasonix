package bot

import (
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func mustBindBotControllerAuthority(t *testing.T, leases *control.SessionLeaseKeeper, ctrl *control.Controller) {
	t.Helper()
	if err := leases.BindControllerAuthority(ctrl); err != nil {
		t.Fatalf("bind controller authority: %v", err)
	}
}

func mustAcquireAndReleaseBotSessionLease(t *testing.T, path string) {
	t.Helper()
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("session lease was not released: %v", err)
	}
	lease.Release()
}
