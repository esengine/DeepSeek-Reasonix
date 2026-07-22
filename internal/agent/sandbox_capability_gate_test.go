package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

type capabilityOrderTool struct {
	order   *[]string
	prepare int
	name    string
}

func (t *capabilityOrderTool) Name() string {
	if t.name != "" {
		return t.name
	}
	return "capability_order"
}
func (t *capabilityOrderTool) Description() string     { return "test" }
func (t *capabilityOrderTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *capabilityOrderTool) ReadOnly() bool          { return false }
func (t *capabilityOrderTool) Execute(context.Context, json.RawMessage) (string, error) {
	panic("prepared invocation must be used")
}
func (t *capabilityOrderTool) Preview(json.RawMessage) (diff.Change, error) {
	return diff.Change{Path: "capability-order.txt", Kind: diff.Modify}, nil
}
func (t *capabilityOrderTool) PrepareSandboxInvocation(context.Context, json.RawMessage) (tool.SandboxCapabilityInvocation, error) {
	t.prepare++
	*t.order = append(*t.order, "prepare")
	return &capabilityOrderInvocation{order: t.order}, nil
}

type capabilityOrderInvocation struct{ order *[]string }

func (i *capabilityOrderInvocation) Review() sandbox.CapabilityReview {
	return sandbox.CapabilityReview{State: sandbox.CapabilityReady, EffectiveDelta: sandbox.CapabilitySet{Network: true}, Authority: sandbox.CapabilityAuthorityStatus{Requested: true, Supported: true}}
}
func (i *capabilityOrderInvocation) SandboxCapabilityRequest() tool.SandboxCapabilityRequest {
	return tool.SandboxCapabilityRequest{Command: "sh -c true"}
}
func (i *capabilityOrderInvocation) Execute(context.Context, sandbox.CapabilityUse) (string, error) {
	*i.order = append(*i.order, "execute")
	return "ok", nil
}
func (i *capabilityOrderInvocation) ExecuteDirect(_ context.Context, _ sandbox.CapabilityUse, executable string, argv []string) (string, error) {
	*i.order = append(*i.order, "direct:"+executable+":"+argv[0])
	return "ok", nil
}

type capabilityOrderPermission struct {
	order *[]string
	allow bool
}

func (g capabilityOrderPermission) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	*g.order = append(*g.order, "permission")
	return g.allow, "denied", nil
}

type capabilityOrderGate struct{ order *[]string }

func (g capabilityOrderGate) Authorize(context.Context, sandboxauth.Request) (sandboxauth.Decision, error) {
	*g.order = append(*g.order, "capability")
	return sandboxauth.Decision{Use: sandbox.AuthorizedDelta}, nil
}

type capabilityDirectGate struct {
	order      *[]string
	executable string
}

type capabilityFallbackGate struct{ order *[]string }

func (g capabilityFallbackGate) Authorize(context.Context, sandboxauth.Request) (sandboxauth.Decision, error) {
	*g.order = append(*g.order, "capability")
	return sandboxauth.Decision{Use: sandbox.BaseOnly, Diagnostic: "policy fallback"}, nil
}

type capabilityErrorGate struct{ order *[]string }

func (g capabilityErrorGate) Authorize(context.Context, sandboxauth.Request) (sandboxauth.Decision, error) {
	*g.order = append(*g.order, "capability")
	return sandboxauth.Decision{}, errors.New("adapter unavailable")
}

type capabilityRecoveryRecorder struct{ observations []RecoveryObservation }

func (r *capabilityRecoveryRecorder) BeforeMutation(context.Context, RecoveryProposal) (RecoveryDecision, error) {
	return RecoveryDecision{Allow: true}, nil
}

type capabilityOrderRecoveryGate struct{ order *[]string }

func (g capabilityOrderRecoveryGate) BeforeMutation(context.Context, RecoveryProposal) (RecoveryDecision, error) {
	*g.order = append(*g.order, "auto_guard")
	return RecoveryDecision{Allow: true}, nil
}
func (capabilityOrderRecoveryGate) ObserveResult(context.Context, RecoveryObservation) string {
	return ""
}
func (r *capabilityRecoveryRecorder) ObserveResult(_ context.Context, observation RecoveryObservation) string {
	r.observations = append(r.observations, observation)
	return ""
}

func (g capabilityDirectGate) Authorize(context.Context, sandboxauth.Request) (sandboxauth.Decision, error) {
	*g.order = append(*g.order, "capability")
	return sandboxauth.Decision{Use: sandbox.AuthorizedDelta, CanonicalExecutable: g.executable, Argv: []string{g.executable, "arg"}}, nil
}

type capabilityOrderHooks struct{ order *[]string }

func (h capabilityOrderHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	*h.order = append(*h.order, "hook")
	return false, ""
}
func (capabilityOrderHooks) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (capabilityOrderHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (capabilityOrderHooks) PostLLMCall(context.Context, string, int) string { return "" }
func (capabilityOrderHooks) HasPostLLMCall() bool                            { return false }
func (capabilityOrderHooks) SubagentStop(context.Context, string)            {}
func (capabilityOrderHooks) PreCompact(context.Context, string) string       { return "" }

type capabilityCompleteOrderHooks struct {
	order     *[]string
	scheduler *SubagentScheduler
	probe     *workspacelease.Owner
	claimSeen bool
	leaseSeen bool
}

func (h *capabilityCompleteOrderHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	*h.order = append(*h.order, "hook")
	h.claimSeen = len(h.scheduler.ActiveWriterClaims()) == 1
	h.probe.BeginRun()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	h.leaseSeen = h.probe.AcquireWrite(ctx) != nil
	cancel()
	h.probe.EndRun()
	return false, ""
}
func (*capabilityCompleteOrderHooks) PostToolUse(context.Context, string, json.RawMessage, string) {
}
func (*capabilityCompleteOrderHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (*capabilityCompleteOrderHooks) PostLLMCall(context.Context, string, int) string { return "" }
func (*capabilityCompleteOrderHooks) HasPostLLMCall() bool                            { return false }
func (*capabilityCompleteOrderHooks) SubagentStop(context.Context, string)            {}
func (*capabilityCompleteOrderHooks) PreCompact(context.Context, string) string       { return "" }

func TestOrdinaryPermissionDenialPrecedesCapabilityPreparation(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	a := New(nil, reg, NewSession(""), Options{Gate: capabilityOrderPermission{order: &order, allow: false}, SandboxCapabilityGate: capabilityOrderGate{order: &order}}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	if !out.blocked || ct.prepare != 0 || len(order) != 1 || order[0] != "permission" {
		t.Fatalf("out=%+v prepare=%d order=%v", out, ct.prepare, order)
	}
}

func TestDeliveryGatePrecedesAutoPermissionAndCapabilityPreparation(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	a := New(nil, reg, NewSession(""), Options{
		DeliveryProfile:       true,
		RecoveryGate:          capabilityOrderRecoveryGate{order: &order},
		Gate:                  capabilityOrderPermission{order: &order, allow: true},
		SandboxCapabilityGate: capabilityOrderGate{order: &order},
	}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	if !out.blocked || ct.prepare != 0 || len(order) != 0 {
		t.Fatalf("delivery gate leaked later work: out=%+v prepare=%d order=%v", out, ct.prepare, order)
	}
}

func TestCapabilityGateUsesPreparedInvocationBeforeHooksAndExecute(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order, name: "bash"}
	reg := tool.NewRegistry()
	reg.Add(ct)
	root, locks := t.TempDir(), t.TempDir()
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewSubagentScheduler(4, 2)
	hooks := &capabilityCompleteOrderHooks{order: &order, scheduler: scheduler, probe: probe}
	a := New(nil, reg, NewSession(""), Options{
		Gate: capabilityOrderPermission{order: &order, allow: true}, SandboxCapabilityGate: capabilityOrderGate{order: &order},
		Hooks: hooks, SandboxWorkspace: root,
		RecoveryGate:    capabilityOrderRecoveryGate{order: &order},
		DeliveryProfile: true, WorkspaceLease: owner, WriteScheduler: scheduler, WriteWorkspaceRoot: root,
	}, event.Discard)
	a.deliveryCriteriaEstablished = true
	a.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	owner.BeginRun()
	defer owner.EndRun()
	a.SetPreEditHook(func(diff.Change) { order = append(order, "checkpoint") })
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{"command":"touch x"}`})
	want := []string{"auto_guard", "permission", "prepare", "capability", "hook", "checkpoint", "execute"}
	if out.blocked || out.errMsg != "" || len(order) != len(want) || !hooks.leaseSeen || !hooks.claimSeen {
		t.Fatalf("out=%+v order=%v lease_seen=%v claim_seen=%v", out, order, hooks.leaseSeen, hooks.claimSeen)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}

func TestCapabilityGateFailureFallsBackToBaseAndContinuesPipeline(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	a := New(nil, reg, NewSession(""), Options{
		Gate: capabilityOrderPermission{order: &order, allow: true}, SandboxCapabilityGate: capabilityErrorGate{order: &order},
		Hooks: capabilityOrderHooks{order: &order}, SandboxWorkspace: t.TempDir(),
	}, event.Discard)
	a.SetPreEditHook(func(diff.Change) { order = append(order, "checkpoint") })
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	want := []string{"permission", "prepare", "capability", "hook", "checkpoint", "execute"}
	if out.blocked || out.errMsg != "" || !strings.Contains(out.output, "adapter unavailable") || strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("out=%+v order=%v want=%v", out, order, want)
	}
}

type capabilityCancelGate struct{}

func (capabilityCancelGate) Authorize(context.Context, sandboxauth.Request) (sandboxauth.Decision, error) {
	return sandboxauth.Decision{Use: sandbox.BaseOnly, Cancel: true, Diagnostic: "user canceled"}, nil
}

func TestCapabilityCancelStopsBeforeHooksCheckpointAndExecute(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	a := New(nil, reg, NewSession(""), Options{
		Gate: capabilityOrderPermission{order: &order, allow: true}, SandboxCapabilityGate: capabilityCancelGate{},
		Hooks: capabilityOrderHooks{order: &order}, SandboxWorkspace: t.TempDir(),
	}, event.Discard)
	a.SetPreEditHook(func(diff.Change) { order = append(order, "checkpoint") })
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	if !out.blocked || out.errMsg != "blocked: sandbox capability request canceled" {
		t.Fatalf("out=%+v", out)
	}
	if strings.Join(order, ",") != "permission,prepare" {
		t.Fatalf("post-cancel work ran: %v", order)
	}
}

func TestReusableGrantConsumesCanonicalDirectExecutionWitness(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	const executable = "/canonical/bin/tool"
	a := New(nil, reg, NewSession(""), Options{
		Gate: capabilityOrderPermission{order: &order, allow: true}, SandboxCapabilityGate: capabilityDirectGate{order: &order, executable: executable},
		SandboxWorkspace: t.TempDir(),
	}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	wantLast := "direct:" + executable + ":" + executable
	if out.blocked || out.errMsg != "" || len(order) == 0 || order[len(order)-1] != wantLast {
		t.Fatalf("out=%+v order=%v", out, order)
	}
}

func TestTaskSubagentSharesCapabilityGateAndNarrowsDelegation(t *testing.T) {
	root := t.TempDir()
	parentWrite := filepath.Join(root, "parent")
	childWrite := filepath.Join(parentWrite, "child")
	if err := os.MkdirAll(childWrite, 0o755); err != nil {
		t.Fatal(err)
	}
	order := []string{}
	gate := capabilityOrderGate{order: &order}
	task := (&TaskTool{workspaceRoot: root}).WithSandboxCapabilityGate(gate)
	ctx := WithSubagentDepth(context.Background(), 1)
	ctx = withSandboxDelegation(ctx, sandboxauth.Delegation{ReadRoots: []string{root}, WriteRoots: []string{parentWrite}})
	opts := task.subagentOptions(ctx, 1, nil, 1, 2, "nested", WritePathSet{Paths: []string{childWrite}, WorkspaceRoot: root})
	if opts.SandboxCapabilityGate == nil || len(opts.SandboxDelegation.ReadRoots) != 1 || len(opts.SandboxDelegation.WriteRoots) != 1 || opts.SandboxDelegation.WriteRoots[0] != childWrite {
		t.Fatalf("options gate=%v delegation=%+v", opts.SandboxCapabilityGate != nil, opts.SandboxDelegation)
	}

	outside := t.TempDir()
	opts = task.subagentOptions(ctx, 1, nil, 1, 2, "nested", WritePathSet{Paths: []string{outside}, WorkspaceRoot: root})
	if len(opts.SandboxDelegation.WriteRoots) != 0 {
		t.Fatalf("outside child claim expanded parent ceiling: %+v", opts.SandboxDelegation)
	}
}

func TestSandboxDelegationForChildNarrowsNestedSkillCeiling(t *testing.T) {
	root := t.TempDir()
	parentRead := filepath.Join(root, "read")
	parentWrite := filepath.Join(root, "write")
	for _, path := range []string{parentRead, parentWrite} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := WithSubagentDepth(context.Background(), 1)
	ctx = withSandboxDelegation(ctx, sandboxauth.Delegation{
		ReadRoots: []string{parentRead}, WriteRoots: []string{parentWrite},
	})

	writer := SandboxDelegationForChild(ctx, root, []string{root})
	if len(writer.ReadRoots) != 1 || writer.ReadRoots[0] != parentRead ||
		len(writer.WriteRoots) != 1 || writer.WriteRoots[0] != parentWrite {
		t.Fatalf("nested writer expanded parent ceiling: %+v", writer)
	}
	readOnly := SandboxDelegationForChild(ctx, root, nil)
	if len(readOnly.ReadRoots) != 1 || readOnly.ReadRoots[0] != parentRead || len(readOnly.WriteRoots) != 0 {
		t.Fatalf("nested read-only child expanded parent ceiling: %+v", readOnly)
	}
}

func TestCapabilityFallbackDiagnosticPreservesActualRecoverySuccess(t *testing.T) {
	order := []string{}
	ct := &capabilityOrderTool{order: &order}
	reg := tool.NewRegistry()
	reg.Add(ct)
	recovery := &capabilityRecoveryRecorder{}
	a := New(nil, reg, NewSession(""), Options{
		Gate: capabilityOrderPermission{order: &order, allow: true}, SandboxCapabilityGate: capabilityFallbackGate{order: &order},
		SandboxWorkspace: t.TempDir(), RecoveryGate: recovery,
	}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{ID: "1", Name: ct.Name(), Arguments: `{}`})
	if out.blocked || out.errMsg != "" || !strings.Contains(out.output, "[sandbox authorization] policy fallback") {
		t.Fatalf("out=%+v", out)
	}
	if len(recovery.observations) != 1 || !recovery.observations[0].Success || recovery.observations[0].Blocked {
		t.Fatalf("recovery observations=%+v", recovery.observations)
	}
}
