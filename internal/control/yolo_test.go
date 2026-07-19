package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// TestAutoApproveToolsStillAutoPlansAndRequiresPlanApproval drives the same
// complex request that TestAutoPlanGateEndToEnd uses, but with YOLO/full access
// on. Tool auto-approval skips tool approvals, not collaboration gates: a complex
// task still drafts a plan and must wait for the user's plan approval.
func TestAutoApproveToolsStillAutoPlansAndRequiresPlanApproval(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the config field\n2. Wire it into boot\n3. Add tests"),
		textTurn("Done — implemented the approved plan."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	approvalRequests := make(chan event.Approval, 1)
	var seeded bool
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalRequests <- e.Approval
			case event.ToolDispatch:
				if e.Tool.ID == "plan-seed" {
					seeded = true
				}
			}
		}),
	})
	c.SetAutoApproveTools(true)

	input := "实现 issue #2395：新增配置项、自动判断复杂任务、补测试和文档"
	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), input, input) }()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("tool auto-approval must not suppress plan approval")
	}
	if approval.Tool != planApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, planApprovalTool)
	}

	if !c.PlanMode() {
		t.Fatal("controller should stay in plan mode while waiting for approval")
	}
	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurnWithRaw: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("approved plan did not continue into execution")
	}
	if got := agent.StripTransientUserBlocks(firstUserMessage(ag.Session().Messages)); !strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("first model input = %q, want the auto-plan marker prefixed", got)
	}
	if c.PlanMode() {
		t.Fatal("plan mode should be off after approval")
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should remain on after plan approval")
	}
	if !seeded {
		t.Fatal("approved plan should seed the task list")
	}
	if prov.call != 2 {
		t.Fatalf("provider called %d times, want 2 (plan + execution)", prov.call)
	}
}

// TestRequestApprovalHonorsAutoApproveTools guards the underlying gate: ordinary
// tool approvals must return allow immediately without emitting anything under
// tool auto-approval.
func TestRequestApprovalHonorsAutoApproveTools(t *testing.T) {
	var approvalRequested bool
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequested = true
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			t.Errorf("requestApproval: %v", err)
		}
		done <- allow
	}()

	select {
	case allow := <-done:
		if !allow {
			t.Fatal("tool auto-approval should allow the approval")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("requestApproval blocked under tool auto-approval")
	}

	if approvalRequested {
		t.Fatal("tool auto-approval must not emit an ApprovalRequest event")
	}
}

func TestMemoryApprovalIgnoresAutoApproveTools(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "remember", "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted under tool auto-approval")
	}
	if approval.Tool != "remember" {
		t.Fatalf("approval tool = %q, want remember", approval.Tool)
	}

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("memory approval must wait for manual approval, got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow memory write")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
	}
}

func TestToolApprovalModeAutoKeepsAskRules(t *testing.T) {
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, []string{"bash(rm*)"}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)

	gate := c.newInteractiveGate()
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"go test ./..."}`)); got != permission.Allow {
		t.Fatalf("auto mode fallback = %v, want allow", got)
	}
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"git commit -m x"}`)); got != permission.Ask {
		t.Fatalf("explicit ask rule = %v, want ask", got)
	}
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"rm -rf build"}`)); got != permission.Deny {
		t.Fatalf("deny rule = %v, want deny", got)
	}
	if c.AutoApproveTools() {
		t.Fatal("auto approval must not report as YOLO")
	}
}

func TestToolApprovalModeAutoForcesMemoryAskRules(t *testing.T) {
	c := New(Options{})
	c.SetToolApprovalMode(ToolApprovalAuto)

	gate := c.newInteractiveGate()
	for _, toolName := range []string{"remember", "forget"} {
		if got := gate.Policy.Decide(toolName, false, json.RawMessage(`{}`)); got != permission.Ask {
			t.Fatalf("%s under auto mode = %v, want ask", toolName, got)
		}
	}
}

func TestToolApprovalModeYoloForcesMemoryAskRules(t *testing.T) {
	c := New(Options{})
	c.SetToolApprovalMode(ToolApprovalYolo)

	gate := c.newInteractiveGate()
	for _, toolName := range []string{"remember", "forget"} {
		if got := gate.Policy.Decide(toolName, false, json.RawMessage(`{}`)); got != permission.Ask {
			t.Fatalf("%s under yolo mode = %v, want ask", toolName, got)
		}
	}
	// Verify that regular tools ARE auto-allowed in YOLO (sanity check).
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"go test ./..."}`)); got != permission.Allow {
		t.Fatalf("regular tool under yolo mode = %v, want allow", got)
	}
}

func TestToolApprovalModeDontAskDeniesWithoutPrompt(t *testing.T) {
	requests := 0
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, nil).
			WithSessionAllow([]string{"bash(go test*)"}),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				requests++
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalDontAsk)
	gate := c.newInteractiveGate()

	allow, _, err := gate.Check(context.Background(), "bash", json.RawMessage(`{"command":"go test ./..."}`), false)
	if err != nil || !allow {
		t.Fatalf("session-allowed call = (%v, %v), want allow", allow, err)
	}
	allow, _, err = gate.Check(context.Background(), "bash", json.RawMessage(`{"command":"git commit -m x"}`), false)
	if err != nil || allow {
		t.Fatalf("explicit ask under dontAsk = (%v, %v), want deny", allow, err)
	}
	allow, _, err = gate.Check(context.Background(), "write_file", json.RawMessage(`{"path":"x.txt"}`), false)
	if err != nil || allow {
		t.Fatalf("fallback under dontAsk = (%v, %v), want deny", allow, err)
	}
	if requests != 0 {
		t.Fatalf("dontAsk emitted %d approval requests, want 0", requests)
	}
}

func TestToolApprovalModeAutoDrainsPendingFallbackApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	select {
	case <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetToolApprovalMode(ToolApprovalAuto)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending fallback approval should be allowed when auto approval turns on")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pending fallback approval stayed blocked after auto approval turned on")
	}
	if c.AutoApproveTools() {
		t.Fatal("auto mode must not report as YOLO")
	}
}

func TestToolApprovalModeAutoDoesNotDrainPendingExplicitAsk(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "bash", "git commit -m x", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetToolApprovalMode(ToolApprovalAuto)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("auto mode must not answer explicit ask rules; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow the explicit ask request")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("explicit ask approval stayed blocked after manual Approve")
	}
}

func TestToolApprovalModeYoloBypassesApprovalPrompts(t *testing.T) {
	c := New(Options{})
	c.SetToolApprovalMode(ToolApprovalYolo)
	if !c.AutoApproveTools() {
		t.Fatal("YOLO mode should satisfy legacy AutoApproveTools")
	}
	allow, remember, err := c.requestApproval(context.Background(), "bash", "go test ./...", nil)
	if err != nil || !allow || remember {
		t.Fatalf("requestApproval in YOLO = (%v,%v,%v), want allow without remember", allow, remember, err)
	}
}

func TestPlanApprovalIgnoresAutoApproveTools(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("plan approval must still prompt under tool auto-approval")
	}
	if approval.Tool != planApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, planApprovalTool)
	}
	select {
	case allow := <-done:
		t.Fatalf("plan approval must wait for the user under tool auto-approval; got allow=%v", allow)
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	default:
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual plan approval should allow")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan approval stayed blocked after Approve")
	}
}

// TestSetAutoApproveToolsAllowsPendingApproval covers the desktop case where the
// approval card is already visible, then the user switches to YOLO/full access.
// Turning tool auto-approval on must unblock that pending tool gate too.
func TestSetAutoApproveToolsAllowsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	select {
	case <-ids:
	case <-time.After(30 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending approval should be allowed when tool auto-approval turns on")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pending approval stayed blocked after tool auto-approval turned on")
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should remain on after draining pending approvals")
	}
}

func TestSandboxEscapeApprovalIgnoresAutoApproveTools(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	type escapeResult struct {
		allow  bool
		reason string
		err    error
	}
	done := make(chan escapeResult, 1)
	go func() {
		allow, reason, err := sandboxEscapeApprover{c}.ApproveSandboxEscape(context.Background(), sandbox.EscapeRequest{
			Command: "go test ./...",
			Reason:  "Windows sandbox failed. Run this command unconfined once?",
		})
		done <- escapeResult{allow: allow, reason: reason, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("sandbox escape approval request was not emitted")
	}
	if approval.Tool != SandboxEscapeApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, SandboxEscapeApprovalTool)
	}

	c.SetAutoApproveTools(true)
	select {
	case got := <-done:
		t.Fatalf("tool auto-approval must not answer sandbox escape; got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.reason != "" {
			t.Fatalf("sandbox escape result = %+v, want allowed without reason/error", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("sandbox escape approval stayed blocked after Approve")
	}

	if !(sandboxEscapeApprover{c}).SandboxEscapeSessionAllowed(context.Background(), sandbox.EscapeRequest{Command: "npm test"}) {
		t.Fatal("sandbox escape session checker = false, want true after session grant")
	}
	allow, reason, err := sandboxEscapeApprover{c}.ApproveSandboxEscape(context.Background(), sandbox.EscapeRequest{
		Command: "npm test",
		Reason:  "Windows sandbox failed. Run this command unconfined once?",
	})
	if err != nil || !allow || reason != "" {
		t.Fatalf("sandbox escape session grant result = (%v,%q,%v), want allow", allow, reason, err)
	}
	select {
	case approval := <-approvalRequests:
		t.Fatalf("sandbox escape session grant emitted another approval: %+v", approval)
	default:
	}
}

func TestDestructiveMCPApprovalAlwaysRequiresCurrentHumanDecision(t *testing.T) {
	for _, mode := range []string{ToolApprovalAuto, ToolApprovalYolo} {
		t.Run(mode, func(t *testing.T) {
			approvals := make(chan event.Approval, 2)
			remembered := 0
			c := New(Options{
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest {
						approvals <- e.Approval
					}
				}),
				OnRemember: func(string) RememberResult {
					remembered++
					return RememberResult{Saved: true}
				},
			})
			c.SetToolApprovalMode(mode)

			call := func() <-chan struct {
				allow  bool
				reason string
				err    error
			} {
				done := make(chan struct {
					allow  bool
					reason string
					err    error
				}, 1)
				go func() {
					allow, reason, err := (gateApprover{c}).ApproveFresh(context.Background(), "mcp__srv__wipe", "srv/wipe", json.RawMessage(`{"target":"all"}`))
					done <- struct {
						allow  bool
						reason string
						err    error
					}{allow: allow, reason: reason, err: err}
				}()
				return done
			}

			for invocation := 1; invocation <= 2; invocation++ {
				done := call()
				var approval event.Approval
				select {
				case approval = <-approvals:
				case <-time.After(30 * time.Second):
					t.Fatalf("invocation %d did not request approval in %s mode", invocation, mode)
				}
				if !approval.Fresh || approval.Tool != "mcp__srv__wipe" {
					t.Fatalf("approval = %+v, want fresh destructive MCP request", approval)
				}
				select {
				case got := <-done:
					t.Fatalf("%s mode auto-answered destructive MCP approval: %+v", mode, got)
				case <-time.After(50 * time.Millisecond):
				}

				// Session/persistent flags from an old frontend must be ignored.
				c.Approve(approval.ID, true, true, true)
				select {
				case got := <-done:
					if got.err != nil || !got.allow || got.reason != "" {
						t.Fatalf("destructive MCP approval = %+v, want one-shot allow", got)
					}
				case <-time.After(30 * time.Second):
					t.Fatal("destructive MCP approval stayed blocked after manual approval")
				}
			}
			if remembered != 0 {
				t.Fatalf("persistent authorization callbacks = %d, want 0", remembered)
			}
		})
	}
}

func TestDestructiveMCPExplicitDenySkipsFreshPrompt(t *testing.T) {
	approvals := make(chan event.Approval, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	gate := permission.NewGate(permission.New("allow", nil, nil, []string{"mcp__srv__wipe"}), gateApprover{c})

	allow, reason, err := gate.CheckFresh(context.Background(), "mcp__srv__wipe", "srv/wipe", nil, false)
	if err != nil || allow || !strings.Contains(reason, "deny list") {
		t.Fatalf("explicit deny result = (%v,%q,%v), want policy denial", allow, reason, err)
	}
	select {
	case approval := <-approvals:
		t.Fatalf("explicit deny emitted approval prompt: %+v", approval)
	default:
	}
}

func TestHeadlessAutoAndYoloRefuseDestructiveMCPFreshApproval(t *testing.T) {
	for _, mode := range []string{ToolApprovalAuto, ToolApprovalYolo} {
		gate := BuildHeadlessApprovalGate(permission.New("allow", nil, nil, nil), mode)
		allow, reason, err := gate.CheckFresh(context.Background(), "mcp__srv__wipe", "srv/wipe", nil, false)
		if err != nil || allow || !strings.Contains(reason, "fresh human approval") {
			t.Fatalf("%s headless fresh check = (%v,%q,%v), want refusal", mode, allow, reason, err)
		}
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingPlanApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("plan approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("SetAutoApproveTools must not auto-answer pending plan approval; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should turn on while plan approval stays pending")
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual plan approval should allow")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan approval stayed blocked after Approve")
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingPlanModeReadOnlyCommandTrust(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	type trustResult struct {
		allow  bool
		reason string
		err    error
	}
	done := make(chan trustResult, 1)
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName: agent.PlanModeReadOnlyCommandApprovalTool,
		Command:  "gh issue view 5867",
		Prefix:   "gh issue view",
	}
	go func() {
		allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
		done <- trustResult{allow: allow, reason: reason, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash read-only command trust approval request was not emitted")
	}
	if approval.Tool != agent.PlanModeReadOnlyCommandApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, agent.PlanModeReadOnlyCommandApprovalTool)
	}

	c.SetAutoApproveTools(true)

	select {
	case got := <-done:
		t.Fatalf("SetAutoApproveTools must not auto-answer plan-mode bash read-only command trust; got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should turn on while plan-mode bash read-only command trust stays pending")
	}

	c.Approve(approval.ID, true, false, false)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.reason != "" {
			t.Fatalf("manual plan-mode bash read-only command trust approval = %+v, want allow", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash read-only command trust approval stayed blocked after Approve")
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingMemoryApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "forget", "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("SetAutoApproveTools must not auto-answer pending memory approval; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow memory archive")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
	}
}

// TestSetModeYoloDrainsPendingApproval is the SetMode-path twin of the
// SetAutoApproveTools case: applying YOLO atomically must also unblock an
// approval already waiting.
func TestSetModeYoloDrainsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		done <- allow
	}()

	select {
	case <-ids:
	case <-time.After(30 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetMode(false, true)

	select {
	case allow := <-done:
		if !allow {
			t.Fatal("pending approval should be auto-allowed when SetMode turns YOLO on")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pending approval stayed blocked after SetMode(false, true)")
	}
}

// TestSetModeAppliesBothGates checks SetMode sets plan and tool auto-approval
// together so the composer never has to sequence two calls and risk a
// half-applied window.
func TestSetModeAppliesBothGates(t *testing.T) {
	c, _, _ := approvalIDs()

	c.SetMode(true, false)
	if !c.PlanMode() || c.AutoApproveTools() {
		t.Fatalf("plan mode: plan=%v autoApproveTools=%v, want true/false", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(false, true)
	if c.PlanMode() || !c.AutoApproveTools() {
		t.Fatalf("yolo mode: plan=%v autoApproveTools=%v, want false/true", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(true, true)
	if !c.PlanMode() || !c.AutoApproveTools() {
		t.Fatalf("plan + yolo mode: plan=%v autoApproveTools=%v, want true/true", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(false, false)
	if c.PlanMode() || c.AutoApproveTools() {
		t.Fatalf("normal mode: plan=%v autoApproveTools=%v, want false/false", c.PlanMode(), c.AutoApproveTools())
	}
}

type planModeCountingRunner struct {
	calls int
	last  bool
}

func (*planModeCountingRunner) Run(context.Context, string) error { return nil }
func (r *planModeCountingRunner) SetPlanMode(v bool) {
	r.calls++
	r.last = v
}

func TestApplyModeUsesRunnerPlanPropagationOnce(t *testing.T) {
	runner := &planModeCountingRunner{}
	c := New(Options{Runner: runner})
	c.ApplyMode(true, true)
	if runner.calls != 1 || !runner.last {
		t.Fatalf("runner SetPlanMode calls=%d last=%v, want 1/true", runner.calls, runner.last)
	}
	if !c.PlanMode() || c.ToolApprovalMode() != ToolApprovalYolo {
		t.Fatalf("controller plan=%v approval=%q, want true/yolo", c.PlanMode(), c.ToolApprovalMode())
	}
	c.SetPlanMode(false)
	if runner.calls != 2 || runner.last {
		t.Fatalf("SetPlanMode runner calls=%d last=%v, want 2/false", runner.calls, runner.last)
	}
}

func TestApplyModePlanPropagationRunnerFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runner func(*agent.Agent) agent.Runner
	}{
		{name: "single agent", runner: func(executor *agent.Agent) agent.Runner { return executor }},
		{name: "runner without setter", runner: func(*agent.Agent) agent.Runner {
			return appendingRunner{session: agent.NewSession("runner")}
		}},
		{name: "nil runner", runner: func(*agent.Agent) agent.Runner { return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			phaseCalls := 0
			reg := tool.NewRegistry()
			reg.Add(plannerUnsafeReadTool{calls: &phaseCalls})
			prov := &scriptedTurns{turns: [][]provider.Chunk{
				toolCallTurn("phase-1", "planner_phase_only", `{}`),
				textTurn("done"),
			}}
			executor := agent.New(prov, reg, agent.NewSession("executor"), agent.Options{}, event.Discard)
			c := New(Options{Runner: tc.runner(executor), Executor: executor})
			c.ApplyMode(true, true)
			if err := executor.Run(context.Background(), "try the execution-phase tool"); err != nil {
				t.Fatalf("executor Run: %v", err)
			}
			if phaseCalls != 0 {
				t.Fatalf("phase-opted-out tool executed %d times, want 0", phaseCalls)
			}
		})
	}
}

type plannerUnsafeReadTool struct {
	calls *int
}

func (plannerUnsafeReadTool) Name() string            { return "planner_phase_only" }
func (plannerUnsafeReadTool) Description() string     { return "planner phase test tool" }
func (plannerUnsafeReadTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (plannerUnsafeReadTool) ReadOnly() bool          { return true }
func (plannerUnsafeReadTool) PlanModeSafe() bool      { return false }
func (t plannerUnsafeReadTool) Execute(context.Context, json.RawMessage) (string, error) {
	(*t.calls)++
	return "executed", nil
}

func TestApplyModePropagatesPlanToCoordinatorPlannerAndKeepsYolo(t *testing.T) {
	plannerCalls := 0
	plannerTools := tool.NewRegistry()
	plannerTools.Add(plannerUnsafeReadTool{calls: &plannerCalls})
	planner := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("planner-tool", "planner_phase_only", `{}`),
		textTurn("1. inspect the current behavior\n2. implement the fix"),
	}}
	execProvider := &scriptedTurns{turns: [][]provider.Chunk{textTurn("executor done")}}
	executor := agent.New(execProvider, tool.NewRegistry(), agent.NewSession("exec"), agent.Options{}, event.Discard)
	coordinator := agent.NewCoordinator(planner, agent.NewSession("planner"), nil, plannerTools, agent.Options{}, executor, 0, event.Discard, nil)
	c := New(Options{Runner: coordinator, Executor: executor})

	c.ApplyMode(true, true)
	if err := c.Run(context.Background(), "prepare the change"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plannerCalls != 0 {
		t.Fatalf("planner phase-only tool executed %d times, want 0 while Plan is active", plannerCalls)
	}
	if !c.PlanMode() || c.ToolApprovalMode() != ToolApprovalYolo {
		t.Fatalf("after run plan=%v approval=%q, want true/yolo", c.PlanMode(), c.ToolApprovalMode())
	}
}

type askCallResult struct {
	answers []event.AskAnswer
	err     error
}

func sampleAskQuestions() []event.AskQuestion {
	return []event.AskQuestion{
		{
			ID:     "approach",
			Header: "Approach",
			Prompt: "Which path?",
			Options: []event.AskOption{
				{Label: "Recommended path"},
				{Label: "Alternative path"},
			},
		},
		{
			ID:     "scope",
			Header: "Scope",
			Prompt: "How broad?",
			Options: []event.AskOption{
				{Label: "Minimal"},
				{Label: "Broad"},
			},
			Multi: true,
		},
	}
}

func askController(t *testing.T, c *Controller, questions []event.AskQuestion) <-chan askCallResult {
	t.Helper()
	done := make(chan askCallResult, 1)
	go func() {
		answers, err := c.Ask(context.Background(), questions)
		done <- askCallResult{answers: answers, err: err}
	}()
	return done
}

func waitAskRequest(t *testing.T, askCh <-chan event.Ask) event.Ask {
	t.Helper()
	select {
	case ask := <-askCh:
		return ask
	case <-time.After(30 * time.Second):
		t.Fatal("Ask did not emit AskRequest")
	}
	return event.Ask{}
}

func waitAskResult(t *testing.T, done <-chan askCallResult) askCallResult {
	t.Helper()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Ask: %v", result.err)
		}
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("Ask stayed blocked")
	}
	return askCallResult{}
}

func assertAskAnswers(t *testing.T, got, want []event.AskAnswer) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("answers len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].QuestionID != want[i].QuestionID || len(got[i].Selected) != len(want[i].Selected) {
			t.Fatalf("answers[%d] = %#v, want %#v", i, got[i], want[i])
		}
		for j := range want[i].Selected {
			if got[i].Selected[j] != want[i].Selected[j] {
				t.Fatalf("answers[%d] = %#v, want %#v", i, got[i], want[i])
			}
		}
	}
}

func TestBypassDoesNotAutoAnswerAsk(t *testing.T) {
	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	c.SetBypass(true)

	done := askController(t, c, sampleAskQuestions())
	ask := waitAskRequest(t, askCh)

	// Even with bypass/YOLO on, Ask must wait for the user's non-default choice.
	c.AnswerQuestion(ask.ID, userAnswers)
	result := waitAskResult(t, done)
	assertAskAnswers(t, result.answers, userAnswers)
}

func TestAskPromptsAcrossInteractiveModes(t *testing.T) {
	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	tests := []struct {
		name  string
		setup func(*Controller)
	}{
		{name: "normal"},
		{name: "plan", setup: func(c *Controller) { c.SetMode(true, false) }},
		{name: "yolo", setup: func(c *Controller) { c.SetMode(false, true) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			askCh := make(chan event.Ask, 1)
			c := New(Options{
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.AskRequest {
						askCh <- e.Ask
					}
				}),
			})
			if tt.setup != nil {
				tt.setup(c)
			}

			done := askController(t, c, sampleAskQuestions())
			ask := waitAskRequest(t, askCh)

			// Answer with non-recommended options to prove this is the user's
			// selection, not an automatic recommended-option fallback.
			c.AnswerQuestion(ask.ID, userAnswers)
			result := waitAskResult(t, done)
			assertAskAnswers(t, result.answers, userAnswers)
		})
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingAsk(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})

	done := askController(t, c, sampleAskQuestions())
	ask := waitAskRequest(t, askCh)

	c.SetAutoApproveTools(true)

	select {
	case result := <-done:
		t.Fatalf("SetAutoApproveTools must not answer pending AskRequest; got %#v", result.answers)
	case <-time.After(50 * time.Millisecond):
	}

	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	c.AnswerQuestion(ask.ID, userAnswers)
	result := waitAskResult(t, done)
	assertAskAnswers(t, result.answers, userAnswers)
}

func TestAskSerializesBehindPromptLockEvenWithAutoApproveTools(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	questions := []event.AskQuestion{{
		ID:     "q1",
		Header: "Choice",
		Prompt: "Which path?",
		Options: []event.AskOption{
			{Label: "Recommended"},
			{Label: "Alternative"},
		},
	}}

	c.approval.promptMu.Lock()
	started := make(chan struct{})
	done := make(chan []event.AskAnswer, 1)
	errs := make(chan error, 1)
	go func() {
		close(started)
		answers, err := c.Ask(context.Background(), questions)
		if err != nil {
			errs <- err
			return
		}
		done <- answers
	}()
	<-started

	// Give the goroutine a chance to reach promptMu, then prove it did not emit
	// AskRequest while another prompt owns the user-decision slot.
	time.Sleep(20 * time.Millisecond)
	select {
	case ask := <-askCh:
		t.Fatalf("AskRequest emitted while promptMu was held: %#v", ask)
	default:
	}

	// Enable tool auto-approval while Ask is queued behind promptMu.
	c.SetAutoApproveTools(true)
	select {
	case ask := <-askCh:
		t.Fatalf("tool auto-approval must not let Ask bypass promptMu; got %#v", ask)
	default:
	}

	// Release the lock — Ask proceeds but must still emit an AskRequest.
	c.approval.promptMu.Unlock()

	var ask event.Ask
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case ask = <-askCh:
	case <-time.After(30 * time.Second):
		t.Fatal("Ask did not emit AskRequest after acquiring promptMu with tool auto-approval on")
	}

	c.AnswerQuestion(ask.ID, []event.AskAnswer{
		{QuestionID: "q1", Selected: []string{"Alternative"}},
	})

	var answers []event.AskAnswer
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case answers = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Ask stayed blocked after AnswerQuestion")
	}
	if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Alternative" {
		t.Fatalf("answers = %#v, want Alternative (user's choice, not auto-recommended)", answers)
	}
}

func TestAskSerializesBehindPromptLockEvenWithBypass(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	questions := []event.AskQuestion{{
		ID:     "q1",
		Header: "Choice",
		Prompt: "Which path?",
		Options: []event.AskOption{
			{Label: "Recommended"},
			{Label: "Alternative"},
		},
	}}

	c.approval.promptMu.Lock()
	done := make(chan []event.AskAnswer, 1)
	errs := make(chan error, 1)
	go func() {
		answers, err := c.Ask(context.Background(), questions)
		if err != nil {
			errs <- err
			return
		}
		done <- answers
	}()

	// Give the goroutine a chance to reach promptMu, then prove it did not emit
	// AskRequest while another prompt owns the user-decision slot.
	time.Sleep(20 * time.Millisecond)
	select {
	case ask := <-askCh:
		t.Fatalf("AskRequest emitted while promptMu was held: %#v", ask)
	default:
	}

	// Enable bypass while Ask is queued behind promptMu.
	c.SetBypass(true)
	// Release the lock — Ask proceeds but must still emit an AskRequest.
	c.approval.promptMu.Unlock()

	// Post-unlock assertion: Ask must emit AskRequest now that it holds the lock.
	var ask event.Ask
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case ask = <-askCh:
	case <-time.After(30 * time.Second):
		t.Fatal("Ask did not emit AskRequest after acquiring promptMu with bypass on; bypass should not suppress ask")
	}

	// Answer and verify we get the user's choice.
	c.AnswerQuestion(ask.ID, []event.AskAnswer{
		{QuestionID: "q1", Selected: []string{"Alternative"}},
	})

	var answers []event.AskAnswer
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case answers = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Ask stayed blocked after AnswerQuestion")
	}
	if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Alternative" {
		t.Fatalf("answers = %#v, want Alternative (user's choice, not auto-recommended)", answers)
	}
}

// TestApplyToolApprovalModeReportsDrainedIDs pins the drain-report contract
// the desktop frontend relies on (#6432): a posture switch returns exactly
// the pending approval ids it auto-allowed, so the UI dismisses those cards
// and keeps the ones still pending here. Fresh user decisions (plan) never
// drain, and auto keeps approvals an allow policy would not cover.
func TestApplyToolApprovalModeReportsDrainedIDs(t *testing.T) {
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, nil),
	})

	autoOKID, autoOKReply := c.approval.register("bash", "go test ./...", "")
	askRuleID, askRuleReply := c.approval.register("bash", "git commit -m x", "")
	planID, planReply := c.approval.registerDecision(planApprovalTool, "", "", true)

	drained := c.ApplyToolApprovalMode(ToolApprovalAuto)
	if len(drained) != 1 || drained[0] != autoOKID {
		t.Fatalf("auto drained = %v, want [%s]", drained, autoOKID)
	}
	select {
	case r := <-autoOKReply:
		if !r.allow {
			t.Fatal("auto-drained approval must be auto-allowed")
		}
	default:
		t.Fatal("auto-drained approval reply not signaled")
	}
	select {
	case <-askRuleReply:
		t.Fatal("explicit ask-rule approval must stay pending under auto")
	default:
	}

	drained = c.ApplyToolApprovalMode(ToolApprovalYolo)
	if len(drained) != 1 || drained[0] != askRuleID {
		t.Fatalf("yolo drained = %v, want [%s]", drained, askRuleID)
	}
	select {
	case r := <-askRuleReply:
		if !r.allow {
			t.Fatal("yolo-drained approval must be auto-allowed")
		}
	default:
		t.Fatal("yolo-drained approval reply not signaled")
	}

	// The fresh plan decision survives both switches and stays pending.
	select {
	case <-planReply:
		t.Fatal("fresh plan approval must never drain on a posture switch")
	default:
	}
	if !c.approval.hasPending() {
		t.Fatalf("plan approval %s should still be pending", planID)
	}
}
