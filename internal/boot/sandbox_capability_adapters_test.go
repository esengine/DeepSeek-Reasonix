package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
)

type bootCapabilityAdapters struct{}

func (*bootCapabilityAdapters) SandboxCapabilityGrants(context.Context, string) ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	return nil, nil
}
func (*bootCapabilityAdapters) SaveSandboxCapabilityGrant(context.Context, sandboxauth.Grant) error {
	return nil
}
func (*bootCapabilityAdapters) AllowSandboxCapability(context.Context, sandboxauth.Request) (bool, string) {
	return true, ""
}
func (*bootCapabilityAdapters) DecideSandboxCapabilityAutoOnce(context.Context, sandboxauth.Request) sandboxauth.AutoOnceDecision {
	return sandboxauth.AutoOnceDecision{Action: sandboxauth.AutoOnceDefer}
}
func (*bootCapabilityAdapters) RecordSandboxCapabilityDecision(context.Context, sandboxauth.AuditRecord) {
}

func TestSandboxCapabilityEngineForOptionsInjectsRuntimeAdapters(t *testing.T) {
	adapters := &bootCapabilityAdapters{}
	engine := sandboxCapabilityEngineForOptions(Options{
		SandboxCapabilityGrantSource: adapters,
		SandboxCapabilityPersister:   adapters,
		SandboxCapabilityPolicyHook:  adapters,
		SandboxCapabilityAutoOnce:    adapters,
		SandboxCapabilityAudit:       adapters,
	})
	if engine.Source != adapters || engine.Persister != adapters || engine.Hook != adapters ||
		engine.AutoOnce != adapters || engine.Audit != adapters {
		t.Fatalf("runtime adapters were not preserved: %+v", engine)
	}
}

func TestProductionSandboxCapabilityAdaptersFillOnlyMissingSeams(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	engine := sandboxCapabilityEngineForOptions(Options{})
	applyProductionSandboxCapabilityAdapters(engine, root)
	if engine.Source == nil || engine.Persister == nil || engine.AutoOnce == nil {
		t.Fatalf("production adapters missing: %+v", engine)
	}

	override := &bootCapabilityAdapters{}
	engine = &sandboxauth.Engine{Source: override, Persister: override, AutoOnce: override}
	applyProductionSandboxCapabilityAdapters(engine, root)
	if engine.Source != override || engine.Persister != override || engine.AutoOnce != override {
		t.Fatal("production wiring replaced caller overrides")
	}
}

func TestProductionSandboxCapabilityAuditEmitsVisibleAndMetadataEvents(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	engine := &sandboxauth.Engine{}
	applyProductionSandboxCapabilityAudit(engine, sink)
	if engine.Audit == nil {
		t.Fatal("default capability audit was not installed")
	}
	engine.Audit.RecordSandboxCapabilityDecision(context.Background(), sandboxauth.AuditRecord{
		Decision: sandboxauth.Decision{Origin: sandboxauth.OriginYOLOAutoOnce}, Visible: true,
	})
	engine.Audit.RecordSandboxCapabilityDecision(context.Background(), sandboxauth.AuditRecord{
		Decision: sandboxauth.Decision{Origin: sandboxauth.OriginProjectGrant},
	})
	if len(events) != 2 {
		t.Fatalf("events=%+v", events)
	}
	if events[0].Code != event.NoticeCodeSandboxCapabilityYOLO || events[0].Level != event.LevelWarn || events[0].SandboxCapabilityAudit == nil {
		t.Fatalf("YOLO audit event=%+v", events[0])
	}
	if events[1].Code != event.NoticeCodeSandboxCapabilityGrant || events[1].Level != event.LevelInfo || events[1].SandboxCapabilityAudit == nil {
		t.Fatalf("grant metadata event=%+v", events[1])
	}
}

func TestProductionSandboxCapabilityAuditPreservesOverride(t *testing.T) {
	override := &bootCapabilityAdapters{}
	engine := &sandboxauth.Engine{Audit: override}
	applyProductionSandboxCapabilityAudit(engine, event.Discard)
	if engine.Audit != override {
		t.Fatal("default audit replaced caller override")
	}
}

func TestBuildHeadlessYOLOEmitsProjectExpansionWarningAtStartup(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	writeFile(t, root, "reasonix.toml", "[sandbox]\nyolo_auto_approve_capabilities = true\n")
	var warnings []event.Event
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: root, HeadlessApprovalMode: control.ToolApprovalYolo,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Code == event.NoticeCodeSandboxCapabilityWarning {
				warnings = append(warnings, e)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if len(warnings) != 1 || warnings[0].SandboxCapabilityWarning == nil || !warnings[0].SandboxCapabilityWarning.Mandatory {
		t.Fatalf("startup warnings=%+v", warnings)
	}
}

func TestBuildHeadlessYOLOSuppressesWarningForSameSessionRebuild(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	writeFile(t, root, "reasonix.toml", "[sandbox]\nyolo_auto_approve_capabilities = true\n")
	var firstWarnings []event.Event
	first, err := Build(context.Background(), Options{
		WorkspaceRoot: root, HeadlessApprovalMode: control.ToolApprovalYolo,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Code == event.NoticeCodeSandboxCapabilityWarning {
				firstWarnings = append(firstWarnings, e)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if len(firstWarnings) != 1 {
		t.Fatalf("first build warnings=%d", len(firstWarnings))
	}
	auth := first.SessionAuthorizations()

	var rebuiltWarnings []event.Event
	rebuilt, err := Build(context.Background(), Options{
		WorkspaceRoot: root, HeadlessApprovalMode: control.ToolApprovalYolo,
		SessionAuthorizations: &auth,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Code == event.NoticeCodeSandboxCapabilityWarning {
				rebuiltWarnings = append(rebuiltWarnings, e)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	if len(rebuiltWarnings) != 0 {
		t.Fatalf("same-session rebuild warnings=%d", len(rebuiltWarnings))
	}
}

func TestBuildHeadlessYOLOHistoryResumeWarningLifecycle(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	writeFile(t, root, "reasonix.toml", "[sandbox]\nyolo_auto_approve_capabilities = true\n")
	var warnings []event.Event
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: root, HeadlessApprovalMode: control.ToolApprovalYolo,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Code == event.NoticeCodeSandboxCapabilityWarning {
				warnings = append(warnings, e)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if len(warnings) != 1 {
		t.Fatalf("build warnings=%d, want 1", len(warnings))
	}

	ctrl.Resume(agent.NewSession("sys"), "")
	if len(warnings) != 1 {
		t.Fatalf("build then initial resume warnings=%d, want 1", len(warnings))
	}

	ctrl.Resume(agent.NewSession("sys"), "")
	if len(warnings) != 2 {
		t.Fatalf("later history resume warnings=%d, want 2", len(warnings))
	}
}

func TestConfiguredPermissionRequestHookCanOnlyDenyCapabilityAuthority(t *testing.T) {
	var stdin string
	runner := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "deny", PayloadFormat: "claude"},
		Event:      hook.PermissionRequest,
	}}, t.TempDir(), func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		stdin = in.Stdin
		return hook.SpawnResult{ExitCode: 0, Stdout: `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny"}}}`}
	}, nil)
	policy := sandboxCapabilityPolicyHooks{configured: runner}
	allow, _ := policy.AllowSandboxCapability(context.Background(), sandboxauth.Request{
		Command: "printf ok",
		Review: sandbox.CapabilityReview{
			EffectiveDelta: sandbox.CapabilitySet{Network: true},
		},
	})
	if allow {
		t.Fatal("configured PermissionRequest deny did not reduce capability authority")
	}
	if !strings.Contains(stdin, `"tool_name":"Bash"`) || !strings.Contains(stdin, "printf ok") || !strings.Contains(stdin, "sandbox_capabilities") {
		t.Fatalf("hook payload=%s", stdin)
	}
}
