package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
	"reasonix/internal/tool"
)

func TestHeadlessYOLOProjectWarningEmitsExactlyOnceOnEntry(t *testing.T) {
	workspace := t.TempDir()
	policy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	engine := &sandboxauth.Engine{AutoOnce: policy}
	var events []event.Event
	c := New(Options{
		WorkspaceRoot: workspace, SandboxCapabilityEngine: engine,
		Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
		Sink:     event.FuncSink(func(e event.Event) { events = append(events, e) }),
	})
	c.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	c.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	var warnings []event.Event
	for _, e := range events {
		if e.Code == event.NoticeCodeSandboxCapabilityWarning {
			warnings = append(warnings, e)
		}
	}
	if len(warnings) != 1 || warnings[0].SandboxCapabilityWarning == nil || !warnings[0].SandboxCapabilityWarning.Mandatory {
		t.Fatalf("startup warnings=%+v", warnings)
	}
}

func TestHeadlessYOLONonExpandedPolicyDoesNotEmitProjectWarning(t *testing.T) {
	workspace := t.TempDir()
	policy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace})
	engine := &sandboxauth.Engine{AutoOnce: policy}
	var warnings int
	c := New(Options{
		WorkspaceRoot: workspace, SandboxCapabilityEngine: engine,
		Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Code == event.NoticeCodeSandboxCapabilityWarning {
				warnings++
			}
		}),
	})
	c.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	c.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	if warnings != 0 {
		t.Fatalf("non-expanded startup warnings=%d", warnings)
	}
}

func TestHeadlessYOLOProjectWarningLifetimeAcrossControllerSessions(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte("[sandbox]\nyolo_auto_approve_capabilities = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newController := func(workspace string, events *[]event.Event) *Controller {
		policy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
		return New(Options{
			WorkspaceRoot: workspace, SandboxCapabilityEngine: &sandboxauth.Engine{AutoOnce: policy},
			Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
			Sink: event.FuncSink(func(e event.Event) {
				if e.Code == event.NoticeCodeSandboxCapabilityWarning {
					*events = append(*events, e)
				}
			}),
		})
	}

	var originalEvents []event.Event
	original := newController(workspace, &originalEvents)
	original.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	if len(originalEvents) != 1 {
		t.Fatalf("first entry warnings=%d", len(originalEvents))
	}

	var rebuiltEvents []event.Event
	rebuilt := newController(workspace, &rebuiltEvents)
	rebuilt.RestoreSessionAuthorizations(original.SessionAuthorizations())
	rebuilt.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	rebuilt.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	if len(rebuiltEvents) != 0 {
		t.Fatalf("same-session rebuild warnings=%d", len(rebuiltEvents))
	}

	if err := rebuilt.NewSession(); err != nil {
		t.Fatalf("new session: %v", err)
	}
	if len(rebuiltEvents) != 1 {
		t.Fatalf("new session warnings=%d", len(rebuiltEvents))
	}
	if err := rebuilt.ClearSession(); err != nil {
		t.Fatalf("clear session: %v", err)
	}
	if len(rebuiltEvents) != 2 {
		t.Fatalf("clear session warnings=%d", len(rebuiltEvents))
	}
	rebuilt.Resume(agent.NewSession("sys"), "")
	if len(rebuiltEvents) != 3 {
		t.Fatalf("resume warnings=%d", len(rebuiltEvents))
	}

	var changedWorkspaceEvents []event.Event
	changedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(changedRoot, "reasonix.toml"), []byte("[sandbox]\nyolo_auto_approve_capabilities = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedWorkspace := newController(changedRoot, &changedWorkspaceEvents)
	changedWorkspace.RestoreSessionAuthorizations(original.SessionAuthorizations())
	changedWorkspace.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	if len(changedWorkspaceEvents) != 1 {
		t.Fatalf("workspace change warnings=%d", len(changedWorkspaceEvents))
	}
}

func TestReloadSandboxCapabilityYOLOProjectExpansionRequiresAcknowledgement(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	workspace := t.TempDir()
	path := filepath.Join(workspace, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[sandbox]\nyolo_auto_approve_capabilities = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := sandboxauth.NewYOLOPolicy(config.ResolveYOLOPolicyConfig(workspace))
	engine := &sandboxauth.Engine{AutoOnce: policy}
	c := New(Options{
		WorkspaceRoot: workspace, SandboxCapabilityEngine: engine,
		Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
	})
	c.EnableInteractiveApproval()
	c.SetToolApprovalMode(ToolApprovalYolo)
	if state, _ := c.SandboxCapabilityYOLOState(); state.Acknowledgement != sandboxauth.YOLONotRequired {
		t.Fatalf("initial state=%+v", state)
	}
	if err := os.WriteFile(path, []byte("[sandbox]\nyolo_auto_approve_capabilities = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, ok := c.ReloadSandboxCapabilityYOLO()
	if !ok || !state.Effective || state.Acknowledgement != sandboxauth.YOLORequired {
		t.Fatalf("reloaded state=%+v ok=%v", state, ok)
	}
	decision := policy.DecideSandboxCapabilityAutoOnce(context.Background(), sandboxauth.Request{})
	if decision.Action != sandboxauth.AutoOnceRunSandboxed {
		t.Fatalf("pending reload decision=%+v", decision)
	}
	if !c.AcknowledgeSandboxCapabilityYOLO(true) {
		t.Fatal("reloaded acknowledgement was not accepted")
	}
}

type controllerCapabilityTool struct {
	mu      sync.Mutex
	uses    []sandbox.CapabilityUse
	directs [][]string
}

func (*controllerCapabilityTool) Name() string { return "capability_e2e" }
func (*controllerCapabilityTool) Description() string {
	return "exercise sandbox capability authorization"
}
func (*controllerCapabilityTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (*controllerCapabilityTool) ReadOnly() bool { return false }
func (*controllerCapabilityTool) Execute(context.Context, json.RawMessage) (string, error) {
	panic("capability tool must execute its prepared invocation")
}
func (t *controllerCapabilityTool) PrepareSandboxInvocation(context.Context, json.RawMessage) (tool.SandboxCapabilityInvocation, error) {
	return &controllerCapabilityInvocation{tool: t}, nil
}

type controllerCapabilityInvocation struct{ tool *controllerCapabilityTool }

func (*controllerCapabilityInvocation) Review() sandbox.CapabilityReview {
	return sandbox.CapabilityReview{
		State:          sandbox.CapabilityReady,
		EffectiveDelta: sandbox.CapabilitySet{Network: true},
		Authority:      sandbox.CapabilityAuthorityStatus{Requested: true, Supported: true},
	}
}
func (*controllerCapabilityInvocation) SandboxCapabilityRequest() tool.SandboxCapabilityRequest {
	return tool.SandboxCapabilityRequest{Command: "printf capability-e2e", ReusableArgv: []string{"printf", "capability-e2e"}}
}
func (i *controllerCapabilityInvocation) Execute(_ context.Context, use sandbox.CapabilityUse) (string, error) {
	i.tool.mu.Lock()
	defer i.tool.mu.Unlock()
	i.tool.uses = append(i.tool.uses, use)
	return "prepared execution", nil
}
func (i *controllerCapabilityInvocation) ExecuteDirect(_ context.Context, use sandbox.CapabilityUse, _ string, argv []string) (string, error) {
	i.tool.mu.Lock()
	defer i.tool.mu.Unlock()
	i.tool.uses = append(i.tool.uses, use)
	i.tool.directs = append(i.tool.directs, append([]string(nil), argv...))
	return "canonical direct execution", nil
}

type controllerCapabilityAudit struct {
	mu      sync.Mutex
	records []sandboxauth.AuditRecord
}

func (a *controllerCapabilityAudit) RecordSandboxCapabilityDecision(_ context.Context, record sandboxauth.AuditRecord) {
	a.mu.Lock()
	a.records = append(a.records, record)
	a.mu.Unlock()
}

func capabilityPromptForTest() sandboxauth.Prompt {
	return sandboxauth.Prompt{Review: sandbox.CapabilityReview{
		State:     sandbox.CapabilityReady,
		Authority: sandbox.CapabilityAuthorityStatus{Requested: true, Supported: true},
	}, Argv: []string{"sh", "-c", "true"}, Reusable: true}
}

func TestSandboxCapabilityPromptSurvivesApprovalModeChanges(t *testing.T) {
	var c *Controller
	seen := make(chan string, 1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest && e.Approval.Kind == sandboxauth.ApprovalKind {
			seen <- e.Approval.ID
		}
	})
	c = New(Options{Sink: sink})
	c.EnableInteractiveApproval()
	var wg sync.WaitGroup
	wg.Add(1)
	result := make(chan sandboxauth.Action, 1)
	go func() {
		defer wg.Done()
		action, _ := c.ApproveSandboxCapability(context.Background(), capabilityPromptForTest())
		result <- action
	}()
	id := <-seen
	c.SetToolApprovalMode(ToolApprovalYolo)
	select {
	case action := <-result:
		t.Fatalf("strict-fresh prompt drained by mode change: %q", action)
	case <-time.After(20 * time.Millisecond):
	}
	if err := c.ResolveSandboxCapability(id, sandboxauth.RunSandboxed); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if action := <-result; action != sandboxauth.RunSandboxed {
		t.Fatalf("action=%q", action)
	}
}

func TestSandboxCapabilityHeadlessControllerFallsBackWithoutPrompt(t *testing.T) {
	c := New(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	action, err := c.ApproveSandboxCapability(ctx, capabilityPromptForTest())
	if err != nil || action != sandboxauth.RunSandboxed {
		t.Fatalf("headless action=%q err=%v", action, err)
	}
}

func TestResolveSandboxCapabilityKeepsInvalidActionPending(t *testing.T) {
	m := newApprovalManager(permission.Policy{}, ToolApprovalAsk, 0)
	id, reply := m.registerSandboxCapability(capabilityPromptForTest())
	if _, ok := m.resolveSandboxCapability(id, sandboxauth.Action("bogus")); ok {
		t.Fatal("invalid action unexpectedly resolved")
	}
	if !m.hasPending() {
		t.Fatal("invalid action consumed waiter")
	}
	ch, ok := m.resolveSandboxCapability(id, sandboxauth.AllowOnce)
	if !ok || ch != reply || m.hasPending() {
		t.Fatalf("valid resolution ok=%v same=%v pending=%v", ok, ch == reply, m.hasPending())
	}
}

func TestSandboxCapabilityRegistersBeforeSynchronousEventResolution(t *testing.T) {
	var c *Controller
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest && e.Approval.Kind == sandboxauth.ApprovalKind {
			if e.Approval.SandboxCapability == nil {
				t.Error("missing authoritative capability payload")
			}
			if err := c.ResolveSandboxCapability(e.Approval.ID, sandboxauth.AllowOnce); err != nil {
				t.Errorf("synchronous resolve: %v", err)
			}
		}
	})
	c = New(Options{Sink: sink})
	c.EnableInteractiveApproval()
	action, err := c.ApproveSandboxCapability(context.Background(), capabilityPromptForTest())
	if err != nil || action != sandboxauth.AllowOnce {
		t.Fatalf("action=%q err=%v", action, err)
	}
}

func TestLegacyBooleanCapabilityApprovalAlwaysRunsSandboxed(t *testing.T) {
	for _, allow := range []bool{false, true} {
		c := New(Options{})
		id, reply := c.approval.registerSandboxCapability(capabilityPromptForTest())
		c.Approve(id, allow, true, true)
		if got := <-reply; got != sandboxauth.RunSandboxed {
			t.Fatalf("allow=%v action=%q", allow, got)
		}
	}
}

func TestSandboxCapabilitySessionGrantsCarryAcrossRebuildButNotResume(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte("[sandbox]\nyolo_auto_approve_capabilities = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	grant := sandboxauth.Grant{Workspace: workspace, CanonicalExecutable: "/bin/tool", ArgvPrefix: []string{"tool"}, Capabilities: sandbox.CapabilitySet{Network: true}}
	oldPolicy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	oldPolicy.Acknowledge(true)
	oldEngine := &sandboxauth.Engine{AutoOnce: oldPolicy}
	oldEngine.RestoreSessionGrants([]sandboxauth.Grant{grant})
	old := New(Options{WorkspaceRoot: workspace, SandboxCapabilityEngine: oldEngine, Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)})

	newPolicy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace, Effective: true, ProjectExpansion: true})
	newEngine := &sandboxauth.Engine{AutoOnce: newPolicy}
	fresh := New(Options{WorkspaceRoot: workspace, SandboxCapabilityEngine: newEngine, Executor: agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)})
	fresh.RestoreSessionAuthorizations(old.SessionAuthorizations())
	if got := len(newEngine.SessionGrants()); got != 1 {
		t.Fatalf("rebuild grants=%d", got)
	}
	if state, _ := newEngine.YOLOState(); state.Acknowledgement != sandboxauth.YOLOAccepted {
		t.Fatalf("rebuild acknowledgement=%+v", state)
	}
	fresh.Resume(agent.NewSession("sys"), "")
	if got := len(newEngine.SessionGrants()); got != 0 {
		t.Fatalf("resume retained %d session grants", got)
	}
	if state, _ := newEngine.YOLOState(); state.Acknowledgement != sandboxauth.YOLORequired {
		t.Fatalf("resume retained acknowledgement=%+v", state)
	}
}

func TestSandboxCapabilityControllerAgentEndToEndSessionGrantReuse(t *testing.T) {
	capabilityTool := &controllerCapabilityTool{}
	registry := tool.NewRegistry()
	registry.Add(capabilityTool)
	provider := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("cap-1", capabilityTool.Name(), `{}`),
		toolCallTurn("cap-2", capabilityTool.Name(), `{}`),
		textTurn("done"),
	}}
	audit := &controllerCapabilityAudit{}
	engine := &sandboxauth.Engine{Audit: audit}

	var controller *Controller
	var approvalKinds []string
	var toolResults []string
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ApprovalRequest:
			approvalKinds = append(approvalKinds, e.Approval.Kind)
			if e.Approval.Kind == sandboxauth.ApprovalKind {
				if err := controller.ResolveSandboxCapability(e.Approval.ID, sandboxauth.AllowSession); err != nil {
					t.Errorf("resolve capability: %v", err)
				}
				return
			}
			controller.Approve(e.Approval.ID, true, true, false)
		case event.ToolResult:
			if e.Tool.Name == capabilityTool.Name() {
				toolResults = append(toolResults, e.Tool.Output)
			}
		}
	})
	agentExecutor := agent.New(provider, registry, agent.NewSession(""), agent.Options{}, sink)
	controller = New(Options{
		Runner: agentExecutor, Executor: agentExecutor, Sink: sink,
		Policy: permission.New("ask", nil, nil, nil), WorkspaceRoot: t.TempDir(),
		SandboxCapabilityEngine: engine,
	})
	controller.EnableInteractiveApproval()

	if err := controller.Run(context.Background(), "exercise capability grants"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(approvalKinds) != 2 || approvalKinds[1] != sandboxauth.ApprovalKind {
		t.Fatalf("approval kinds=%v, want ordinary then sandbox capability", approvalKinds)
	}
	capabilityTool.mu.Lock()
	uses := append([]sandbox.CapabilityUse(nil), capabilityTool.uses...)
	directs := append([][]string(nil), capabilityTool.directs...)
	capabilityTool.mu.Unlock()
	if len(uses) != 2 || uses[0] != sandbox.AuthorizedDelta || uses[1] != sandbox.AuthorizedDelta {
		t.Fatalf("sandbox uses=%v", uses)
	}
	if len(directs) != 1 || len(directs[0]) == 0 || directs[0][0] == "printf" {
		t.Fatalf("canonical direct executions=%v", directs)
	}
	if len(toolResults) != 2 || toolResults[0] != "prepared execution" || toolResults[1] != "canonical direct execution" {
		t.Fatalf("tool results=%v", toolResults)
	}
	audit.mu.Lock()
	records := append([]sandboxauth.AuditRecord(nil), audit.records...)
	audit.mu.Unlock()
	if len(records) != 2 || records[0].Decision.CanonicalExecutable != "" || records[1].Decision.CanonicalExecutable == "" {
		t.Fatalf("audit records=%+v", records)
	}
}
