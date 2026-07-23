package control

import (
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/permission"
)

// TestManagedConfigWriteApprovalIsFreshHuman pins the security contract of the
// managed-config write prompt: it is a fresh human decision, so YOLO/auto
// approval postures must never answer it, while an explicit session grant for
// the same subject may.
func TestManagedConfigWriteApprovalIsFreshHuman(t *testing.T) {
	if !RequiresFreshHumanApprovalTool(ManagedConfigWriteApprovalTool) {
		t.Fatal("config_write must require a fresh human approval")
	}
	if !allowsFreshSessionGrantTool(ManagedConfigWriteApprovalTool) {
		t.Fatal("config_write should allow explicit session grants for one repair flow")
	}

	a := newApprovalManager(permission.Policy{}, ToolApprovalYolo, time.Minute)
	subject := "write Reasonix config: /home/u/.reasonix/config.toml"
	if a.preApprovedForDecision(ManagedConfigWriteApprovalTool, subject, nil, true) {
		t.Fatal("YOLO posture must not pre-approve a managed config write")
	}
	a.grantSession(ManagedConfigWriteApprovalTool, subject)
	if !a.preApprovedForDecision(ManagedConfigWriteApprovalTool, subject, nil, true) {
		t.Fatal("an explicit session grant should cover the same subject")
	}
	// Session grants for fresh decisions are tool-wide (mirroring
	// sandbox_escape): one "allow for this session" covers the rest of the
	// repair flow across the handful of managed config files.
	if !a.preApprovedForDecision(ManagedConfigWriteApprovalTool, "write Reasonix config: /other/path", nil, true) {
		t.Fatal("session grant should cover the repair flow tool-wide")
	}
	// But it must never leak to a different fresh-decision tool.
	if a.preApprovedForDecision(SandboxEscapeApprovalTool, "run unconfined once: rm -rf /", nil, true) {
		t.Fatal("config_write session grant must not answer sandbox_escape decisions")
	}
}

func TestApprovedPlanAutoAllowsFallbackButPreservesExplicitRules(t *testing.T) {
	a := newApprovalManager(
		permission.New("ask", nil, []string{"sensitive_writer", "Edit(secret.txt)"}, []string{"denied_writer"}),
		ToolApprovalAsk,
		time.Minute,
	)
	a.setPlanAutoApprove(true)

	if !a.preApproved("ordinary_writer", "ordinary.txt", json.RawMessage(`{"path":"ordinary.txt"}`)) {
		t.Fatal("an approved plan should auto-allow the ordinary writer fallback")
	}
	if a.preApproved("sensitive_writer", "sensitive.txt", json.RawMessage(`{"path":"sensitive.txt"}`)) {
		t.Fatal("an approved plan must not bypass an explicit ask rule")
	}
	moveArgs := json.RawMessage(`{"source_path":"ordinary.txt","destination_path":"secret.txt"}`)
	if a.preApproved("move_file", "ordinary.txt", moveArgs) {
		t.Fatal("an approved plan must evaluate every subject before bypassing an explicit ask rule")
	}
	if a.preApproved("denied_writer", "denied.txt", json.RawMessage(`{"path":"denied.txt"}`)) {
		t.Fatal("an approved plan must not pre-approve an explicit deny rule")
	}
}

// TestHeadlessGateRefusesManagedConfigApproval pins that the non-interactive
// gate cannot silently answer the config_write decision the way it resolves
// ordinary Ask permissions.
func TestHeadlessGateRefusesManagedConfigApproval(t *testing.T) {
	gate := NewHeadlessPermissionGate(permission.Policy{Mode: permission.Ask})
	allow, _, err := gate.Check(t.Context(), ManagedConfigWriteApprovalTool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if allow {
		t.Fatal("headless gate must refuse fresh-human config_write approvals")
	}
}
