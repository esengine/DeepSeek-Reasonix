package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/hook"
	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
)

type bootCapabilityAdapters struct{}

func (*bootCapabilityAdapters) SandboxCapabilityGrants(context.Context, string) ([]sandboxauth.Grant, []string) {
	return nil, nil
}
func (*bootCapabilityAdapters) SaveSandboxCapabilityGrant(context.Context, sandboxauth.Grant) error {
	return nil
}
func (*bootCapabilityAdapters) AllowSandboxCapability(context.Context, sandboxauth.Request) (bool, string) {
	return true, ""
}
func (*bootCapabilityAdapters) AutoApproveSandboxCapabilityOnce(context.Context, sandboxauth.Request) bool {
	return false
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
