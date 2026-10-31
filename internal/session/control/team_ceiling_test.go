package control

import (
	"testing"

	"reasonix/internal/safety/permission"
)

func TestATurnCeilingOnlyLowersThePostureAndEndsWithTheTurn(t *testing.T) {
	a := newApprovalManager(permission.Policy{}, ToolApprovalYolo, 0, false)
	if a.holdCeiling(ToolApprovalYolo) || a.effectiveMode() != ToolApprovalYolo {
		t.Fatalf("a ceiling no stricter than the posture changed it: %s", a.effectiveMode())
	}
	if !a.holdCeiling(ToolApprovalAsk) || a.effectiveMode() != ToolApprovalAsk || a.mode() != ToolApprovalYolo {
		t.Fatalf("ceiling ask on yolo: effective %s, session %s", a.effectiveMode(), a.mode())
	}
	if a.holdCeiling(ToolApprovalAuto) || a.effectiveMode() != ToolApprovalAsk {
		t.Fatalf("a wider ceiling loosened a held one: %s", a.effectiveMode())
	}
	a.mu.Lock()
	bypass := a.bypassAllowsLocked("write_file", "", nil)
	human := a.requiredHumanAnsweredLocked("write_file", "")
	a.mu.Unlock()
	if bypass || human {
		t.Fatalf("YOLO stood in for a person under an ask ceiling: bypass %v, human %v", bypass, human)
	}
	a.setMode(ToolApprovalYolo)
	if a.effectiveMode() != ToolApprovalAsk {
		t.Fatalf("a mode switch dropped the turn's ceiling: %s", a.effectiveMode())
	}
	if !a.releaseCeiling() || a.effectiveMode() != ToolApprovalYolo || a.releaseCeiling() {
		t.Fatalf("after the turn: %s", a.effectiveMode())
	}
}

func TestASwitchToYoloUnderACeilingDrainsNothing(t *testing.T) {
	a := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0, false)
	a.holdCeiling(ToolApprovalAsk)
	_, _ = a.register("write_file", "x.txt", "write?")
	a.holdCeiling(ToolApprovalReadOnly)
	if drained := a.setMode(ToolApprovalYolo); len(drained) != 0 {
		t.Fatalf("switching to YOLO under a ceiling auto-allowed %d pending approvals", len(drained))
	}
}

func TestDispatchGrantPreservesTheTeamTurnCeiling(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetToolApprovalMode(ToolApprovalYolo)
	c.holdTurnCeiling(ToolApprovalAsk)
	if got := c.DispatchGrant().Approval; got != ToolApprovalAsk {
		t.Fatalf("dispatch grant = %q, want ask", got)
	}
	c.releaseTurnCeiling()
	if got := c.DispatchGrant().Approval; got != ToolApprovalYolo {
		t.Fatalf("ordinary dispatch grant = %q, want yolo", got)
	}
}
