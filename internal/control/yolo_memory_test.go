package control

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/permission"
)

func TestMemoryApprovalStillPromptsUnderAsk(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalAsk)

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
		t.Fatal("memory approval request was not emitted under ask approval")
	}
	if approval.Tool != "remember" {
		t.Fatalf("approval tool = %q, want remember", approval.Tool)
	}

	if !c.PendingPrompt() {
		t.Fatal("memory approval must remain pending under ask")
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

func TestAutoAndYoloAllowMemoryWithoutPrompt(t *testing.T) {
	feedback := json.RawMessage(`{"name":"python-env-cross-project","type":"feedback","description":"cross-project Python env","body":"Embedded Python is unreliable."}`)
	for _, mode := range []string{ToolApprovalAuto, ToolApprovalYolo} {
		t.Run(mode, func(t *testing.T) {
			var approvalRequested bool
			c := New(Options{
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest {
						approvalRequested = true
					}
				}),
			})
			c.SetToolApprovalMode(mode)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for _, toolName := range []string{"remember", "forget"} {
				allow, remember, err := c.requestApproval(ctx, toolName, "", feedback)
				if err != nil || !allow || remember {
					t.Fatalf("requestApproval(%s) in %s = (%v,%v,%v), want allow without persist", toolName, mode, allow, remember, err)
				}
			}

			gate := c.newInteractiveGate()
			for _, toolName := range []string{"remember", "forget"} {
				allow, reason, err := gate.Check(context.Background(), toolName, feedback, false)
				if err != nil || !allow {
					t.Fatalf("interactive gate %s under %s = (%v,%q,%v), want allow", toolName, mode, allow, reason, err)
				}
				if got := gate.Policy.Decide(toolName, false, feedback); got != permission.Allow {
					t.Fatalf("%s policy under %s = %v, want allow", toolName, mode, got)
				}
			}
			if approvalRequested {
				t.Fatalf("%s must not emit a remember/forget approval prompt", mode)
			}
		})
	}
}

func TestToolApprovalModeAutoPreservesExplicitMemoryAsk(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"remember"}, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	gate := c.newInteractiveGate()
	args := json.RawMessage(`{"name":"explicit-review","description":"review","body":"ask first"}`)
	if got := gate.Policy.Decide("remember", false, args); got != permission.Ask {
		t.Fatalf("explicit remember ask under auto = %v, want ask", got)
	}

	done := make(chan bool, 1)
	go func() {
		allow, _, err := gate.Check(context.Background(), "remember", args, false)
		if err != nil {
			done <- false
			return
		}
		done <- allow
	}()
	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("explicit memory ask was not emitted under auto")
	}
	c.Approve(approval.ID, true, false, false)
	select {
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow the explicit memory ask")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("explicit memory ask stayed blocked after approval")
	}
}

func TestToolApprovalModeYoloBypassesMemoryAskAndHonorsDeny(t *testing.T) {
	var approvalRequested bool
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"forget"}, []string{"remember"}),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequested = true
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalYolo)

	gate := c.newInteractiveGate()
	if got := gate.Policy.Decide("forget", false, json.RawMessage(`{}`)); got != permission.Ask {
		t.Fatalf("explicit forget rule under yolo mode = %v, want ask before bypass", got)
	}
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"go test ./..."}`)); got != permission.Allow {
		t.Fatalf("regular tool under yolo mode = %v, want allow", got)
	}
	allow, reason, err := gate.Check(context.Background(), "forget", json.RawMessage(`{"id":"old-fact"}`), false)
	if err != nil || !allow {
		t.Fatalf("explicit forget ask under yolo = (%v,%q,%v), want bypass", allow, reason, err)
	}
	if approvalRequested {
		t.Fatal("YOLO must bypass an explicit remember/forget ask without emitting a prompt")
	}
	allow, _, err = gate.Check(context.Background(), "remember", json.RawMessage(`{"name":"blocked","body":"no"}`), false)
	if err != nil || allow {
		t.Fatalf("denied remember under yolo = (%v,%v), want deny", allow, err)
	}
}

func TestSetAutoApproveToolsDrainsPendingMemoryApproval(t *testing.T) {
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

	select {
	case <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending memory approval should be allowed when YOLO turns on")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pending memory approval stayed blocked after YOLO turned on")
	}
}

func TestToolApprovalModeAutoDrainsPendingMemoryFallback(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := c.requestApproval(context.Background(), "forget", "", nil)
		done <- allow
	}()
	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted")
	}

	drained := c.ApplyToolApprovalMode(ToolApprovalAuto)
	if len(drained) != 1 || drained[0] != approval.ID {
		t.Fatalf("auto drained ids = %v, want [%s]", drained, approval.ID)
	}
	select {
	case allow := <-done:
		if !allow {
			t.Fatal("auto should allow a pending fallback memory approval")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pending fallback memory approval stayed blocked under auto")
	}
}

func TestToolApprovalModeAutoKeepsPendingExplicitMemoryAsk(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"forget"}, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := c.requestApproval(context.Background(), "forget", "", nil)
		done <- allow
	}()
	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("explicit memory ask was not emitted")
	}

	if drained := c.ApplyToolApprovalMode(ToolApprovalAuto); len(drained) != 0 {
		t.Fatalf("auto drained explicit memory ask ids = %v, want none", drained)
	}
	approvals, _ := c.approval.snapshotPrompts()
	if len(approvals) != 1 || approvals[0].ID != approval.ID {
		t.Fatalf("pending approvals after auto = %+v, want %s", approvals, approval.ID)
	}
	c.Approve(approval.ID, true, false, false)
	select {
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow the explicit memory ask")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("explicit memory ask stayed blocked after approval")
	}
}
