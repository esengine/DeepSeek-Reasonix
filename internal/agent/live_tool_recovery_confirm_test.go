//go:build live

package agent

import (
	"context"
	"testing"

	"reasonix/internal/provider"
)

// File postconditions establish current state, not a historical tool outcome.
// Model the host's explicit inspection and user confirmation before Continue.
func confirmLiveWriteRecovery(t *testing.T, ctx context.Context, a *Agent) {
	t.Helper()
	pending := a.PendingToolRecovery()
	if len(pending) != 1 || pending[0].ReadOnly {
		t.Fatalf("expected one unresolved write, got %d recovery records", len(pending))
	}
	id := pending[0].Identity.AttemptID
	proof, err := a.InspectToolRecovery(ctx, id)
	if err != nil || proof.InspectionState != "postcondition_satisfied" {
		t.Fatalf("write inspection: state=%s err=%v", proof.InspectionState, err)
	}
	if err := a.ResolveToolRecovery(id, proof.InspectionID, "confirm"); err != nil {
		t.Fatal(err)
	}
	call, err := a.recoveryCall(id)
	if err != nil || call.Recovery == nil || call.Recovery.State != provider.ToolRunUserConfirmed || len(a.PendingToolRecovery()) != 0 {
		t.Fatal("explicit confirmation did not resolve the exact write attempt")
	}
}
