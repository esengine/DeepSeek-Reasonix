package bot

import (
	"io"
	"log/slog"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/sandbox"
)

func (c *blockingApprovalController) ResolveApproval(id string, allow bool, scope sandbox.ApprovalScope) error {
	c.Approve(id, allow, scope != sandbox.ApprovalScopeOnce, scope == sandbox.ApprovalScopeProject)
	return nil
}

func TestGatewayNormalizesWriteAccessShortcuts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(GatewayConfig{}, nil, logger)
	key := "session-key"
	gw.controllers[key] = &sessionState{
		pendingApprovals: map[string]event.Approval{
			"7": {ID: "7", Tool: "bash", Kind: event.ApprovalKindWriteAccess},
		},
		lastApprovalID: "7",
	}
	got, ok := gw.normalizeApprovalShortcut(key, "2")
	if !ok || got != "/approve-session 7" {
		t.Fatalf("write-access 2 = %q,%v; want /approve-session 7", got, ok)
	}
	got, ok = gw.normalizeApprovalShortcut(key, "3")
	if !ok || got != "/approve-project 7" {
		t.Fatalf("write-access 3 = %q,%v; want /approve-project 7", got, ok)
	}
	got, ok = gw.normalizeApprovalShortcut(key, "4")
	if !ok || got != "/deny 7" {
		t.Fatalf("write-access 4 = %q,%v; want /deny 7", got, ok)
	}
}
