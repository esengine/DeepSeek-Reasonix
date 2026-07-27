package sandboxauth

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

func TestYOLOLifecycleExtensionIsCohesiveAndOptional(t *testing.T) {
	var _ yoloPolicyLifecycle = (*YOLOPolicy)(nil)
	minimal := &allowAutoOnce{}
	if _, ok := any(minimal).(yoloPolicyLifecycle); ok {
		t.Fatal("minimal AutoOncePolicy unexpectedly satisfies the lifecycle extension")
	}
	engine := &Engine{AutoOnce: minimal}
	engine.SetYOLORuntimeMode(true, true)
	if _, ok := engine.YOLOState(); ok {
		t.Fatal("minimal AutoOncePolicy exposed lifecycle state")
	}
}

func TestYOLOPolicyMatrixAndAcknowledgementLifetime(t *testing.T) {
	workspace := t.TempDir()
	p := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	p.SetRuntimeMode(false, true)
	if got := p.DecideSandboxCapabilityAutoOnce(context.Background(), Request{}); got.Action != AutoOnceDefer {
		t.Fatalf("non-YOLO action=%q", got.Action)
	}
	p.SetRuntimeMode(true, true)
	if got := p.DecideSandboxCapabilityAutoOnce(context.Background(), Request{}); got.Action != AutoOnceRunSandboxed || !strings.Contains(got.Diagnostic, "requires acknowledgement") {
		t.Fatalf("pending action=%+v", got)
	}
	if !p.Acknowledge(true) {
		t.Fatal("accept did not consume pending acknowledgement")
	}
	if got := p.DecideSandboxCapabilityAutoOnce(context.Background(), Request{}); got.Action != AutoOnceAllow {
		t.Fatalf("accepted action=%+v", got)
	}

	state := p.SessionState()
	rebuilt := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	rebuilt.RestoreSessionState(state)
	if rebuilt.State().Acknowledgement != YOLOAccepted {
		t.Fatalf("same-session rebuild state=%+v", rebuilt.State())
	}
	different := NewYOLOPolicy(YOLOPolicyConfig{Workspace: t.TempDir(), Effective: true, ProjectExpansion: true})
	different.RestoreSessionState(state)
	if different.State().Acknowledgement != YOLORequired {
		t.Fatalf("workspace change restored authority: %+v", different.State())
	}
}

func TestYOLOPolicyRefusalAndHeadlessProjectWarning(t *testing.T) {
	p := NewYOLOPolicy(YOLOPolicyConfig{Workspace: t.TempDir(), Effective: true, ProjectExpansion: true})
	p.SetRuntimeMode(true, true)
	if !p.Acknowledge(false) {
		t.Fatal("refusal was not recorded")
	}
	if got := p.DecideSandboxCapabilityAutoOnce(context.Background(), Request{}); got.Action != AutoOnceRunSandboxed || !strings.Contains(got.Diagnostic, "refused") {
		t.Fatalf("refused action=%+v", got)
	}

	headless := NewYOLOPolicy(YOLOPolicyConfig{Workspace: t.TempDir(), Effective: true, ProjectExpansion: true})
	headless.SetRuntimeMode(true, false)
	got := headless.DecideSandboxCapabilityAutoOnce(context.Background(), Request{})
	if got.Action != AutoOnceAllow || len(got.Warnings) != 0 {
		t.Fatalf("headless action=%+v", got)
	}
	warnings := headless.TakeStartupWarnings()
	if len(warnings) != 1 || !warnings[0].Mandatory || warnings[0].Code != "yolo_project_capability_expansion" || len(headless.TakeStartupWarnings()) != 0 {
		t.Fatalf("headless startup warnings=%+v", warnings)
	}
	if state := headless.State(); len(state.Warnings) != 1 {
		t.Fatalf("startup warning state=%+v", state)
	}

	userGlobal := NewYOLOPolicy(YOLOPolicyConfig{Workspace: t.TempDir(), Effective: true})
	userGlobal.SetRuntimeMode(true, false)
	if got := userGlobal.DecideSandboxCapabilityAutoOnce(context.Background(), Request{}); got.Action != AutoOnceAllow || len(got.Warnings) != 0 {
		t.Fatalf("user-global true=%+v", got)
	}
}

func TestYOLOPolicyWarningDeliveryLifetime(t *testing.T) {
	workspace := t.TempDir()
	original := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	original.SetRuntimeMode(true, false)
	if got := len(original.TakeStartupWarnings()); got != 1 {
		t.Fatalf("first entry warnings=%d", got)
	}
	carried := original.SessionState()
	if !carried.StartupWarningDelivered {
		t.Fatal("delivered warning was not captured in ephemeral session state")
	}

	rebuilt := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	rebuilt.RestoreSessionState(carried)
	rebuilt.SetRuntimeMode(true, false)
	if got := rebuilt.TakeStartupWarnings(); len(got) != 0 {
		t.Fatalf("same-session rebuild warnings=%+v", got)
	}
	if got := rebuilt.State().Acknowledgement; got != YOLORequired {
		t.Fatalf("delivery marker changed acknowledgement authority to %q", got)
	}
	rebuilt.Configure(YOLOPolicyConfig{Workspace: workspace})
	if rebuilt.SessionState().StartupWarningDelivered {
		t.Fatal("disabled expansion retained a stale warning-delivery marker")
	}
	rebuilt.Configure(YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	if got := len(rebuilt.TakeStartupWarnings()); got != 1 {
		t.Fatalf("false-to-true reload warnings=%d", got)
	}

	rebuilt.ClearSessionState()
	if got := len(rebuilt.TakeStartupWarnings()); got != 1 {
		t.Fatalf("new logical session warnings=%d", got)
	}

	different := NewYOLOPolicy(YOLOPolicyConfig{Workspace: t.TempDir(), Effective: true, ProjectExpansion: true})
	different.RestoreSessionState(carried)
	different.SetRuntimeMode(true, false)
	if got := len(different.TakeStartupWarnings()); got != 1 {
		t.Fatalf("workspace change warnings=%d", got)
	}

	notExpanded := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: false})
	notExpanded.SetRuntimeMode(true, false)
	if got := notExpanded.TakeStartupWarnings(); len(got) != 0 {
		t.Fatalf("disabled policy warnings=%+v", got)
	}
}

func TestEngineYOLOFallbackSkipsPromptAndAuditsOrigin(t *testing.T) {
	workspace := t.TempDir()
	req := Request{Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}, Review: sandbox.CapabilityReview{State: sandbox.CapabilityReady, EffectiveDelta: sandbox.CapabilitySet{Network: true}, Authority: sandbox.CapabilityAuthorityStatus{Requested: true, Supported: true}}}
	approver := &actionApprover{action: AllowOnce}
	audit := &memoryAudit{}
	disabled := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace})
	disabled.SetRuntimeMode(true, true)
	engine := &Engine{Approver: approver, AutoOnce: disabled, Audit: audit}
	decision, err := engine.Authorize(context.Background(), req)
	if err != nil || decision.Use != sandbox.BaseOnly || decision.Origin != OriginBaseSandbox {
		t.Fatalf("disabled decision=%+v err=%v", decision, err)
	}
	if approver.calls != 0 {
		t.Fatal("YOLO fallback opened a capability prompt")
	}

	enabled := NewYOLOPolicy(YOLOPolicyConfig{Workspace: workspace, Effective: true})
	enabled.SetRuntimeMode(true, true)
	engine.AutoOnce = enabled
	decision, err = engine.Authorize(context.Background(), req)
	if err != nil || decision.Use != sandbox.AuthorizedDelta || decision.Origin != OriginYOLOAutoOnce {
		t.Fatalf("enabled decision=%+v err=%v", decision, err)
	}
	if len(audit.records) != 2 || !audit.records[1].Visible {
		t.Fatalf("audit records=%+v", audit.records)
	}
}
