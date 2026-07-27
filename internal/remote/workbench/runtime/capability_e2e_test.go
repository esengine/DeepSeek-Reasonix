package runtime

import (
	"context"
	"net"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/sandboxauth"
)

// TestRuntimeE2ESandboxCapabilityApproval exercises the sandbox capability
// prompt/approve path through a real rpcwire connection (net.Pipe + serveConn).
func TestRuntimeE2ESandboxCapabilityApproval(t *testing.T) {
	workspace := t.TempDir()
	srv := New(Options{Workspace: workspace})
	ctrl := &capabilityTrackingController{fakeController: &fakeController{model: "local/test"}}
	target := srv.installTestSession(ctrl)

	// Set up a pending sandbox capability prompt in the session.
	srv.mu.Lock()
	sess := srv.sessions[target.SessionID]
	sess.pendingPrompt = &protocol.PendingPrompt{
		Kind: protocol.PromptCapabilityApproval,
		Approval: &protocol.ApprovalPrompt{
			PromptID: "cap-e2e-1", Tool: "bash", Subject: "pip install",
			AllowedDecisions: []protocol.PromptDecision{
				protocol.DecisionAllowOnce, protocol.DecisionRunSandboxed,
				protocol.DecisionCancelCommand,
			},
		},
	}
	srv.mu.Unlock()

	// Open a real rpcwire connection.
	hostSide, desktopSide := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go srv.serveConn(ctx, hostSide)

	wire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
		Name: "e2e-test", StrictJSONRPC: true,
		MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	go func() { _ = wire.Serve(ctx) }()

	// Initialize the connection.
	_, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: srv.buildID, ClientInstanceID: "e2e-cap-test", Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Send prompt/approve over the real wire with allow_once.
	_, err = wire.Request(ctx, string(protocol.MethodPromptApprove), protocol.PromptApproveParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "req-cap-1", ExpectedHostEpoch: srv.hostEpoch,
			Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		PromptID: "cap-e2e-1",
		Decision: protocol.DecisionAllowOnce,
	})
	if err != nil {
		t.Fatalf("prompt/approve: %v", err)
	}

	// Verify the controller received the capability resolution.
	if len(ctrl.resolveCapCalls) != 1 {
		t.Fatalf("ResolveSandboxCapability calls = %d, want 1", len(ctrl.resolveCapCalls))
	}
	if ctrl.resolveCapCalls[0].action != sandboxauth.AllowOnce {
		t.Fatalf("action = %q, want %q", ctrl.resolveCapCalls[0].action, sandboxauth.AllowOnce)
	}
	if ctrl.resolveCapCalls[0].id != "cap-e2e-1" {
		t.Fatalf("id = %q, want cap-e2e-1", ctrl.resolveCapCalls[0].id)
	}

	// Verify the pending prompt was cleared.
	srv.mu.Lock()
	pending := sess.pendingPrompt
	srv.mu.Unlock()
	if pending != nil {
		t.Fatal("pending prompt was not cleared")
	}

	// Also verify that legacy Approve was NOT called.
	if len(ctrl.approveCalls) != 0 {
		t.Fatalf("Approve calls = %d, want 0", len(ctrl.approveCalls))
	}
}

// TestRuntimeE2ESandboxCapabilityDeny verifies that /deny maps to RunSandboxed.
func TestRuntimeE2ESandboxCapabilityDeny(t *testing.T) {
	workspace := t.TempDir()
	srv := New(Options{Workspace: workspace})
	ctrl := &capabilityTrackingController{fakeController: &fakeController{model: "local/test"}}
	target := srv.installTestSession(ctrl)

	srv.mu.Lock()
	sess := srv.sessions[target.SessionID]
	sess.pendingPrompt = &protocol.PendingPrompt{
		Kind: protocol.PromptCapabilityApproval,
		Approval: &protocol.ApprovalPrompt{
			PromptID: "cap-deny-1", Tool: "bash", Subject: "rm -rf",
			AllowedDecisions: []protocol.PromptDecision{protocol.DecisionDeny, protocol.DecisionRunSandboxed},
		},
	}
	srv.mu.Unlock()

	hostSide, desktopSide := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go srv.serveConn(ctx, hostSide)

	wire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
		Name: "e2e-deny-test", StrictJSONRPC: true,
		MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	go func() { _ = wire.Serve(ctx) }()

	_, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: srv.buildID, ClientInstanceID: "e2e-cap-deny", Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	_, err = wire.Request(ctx, string(protocol.MethodPromptApprove), protocol.PromptApproveParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "req-deny-1", ExpectedHostEpoch: srv.hostEpoch,
			Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		PromptID: "cap-deny-1",
		Decision: protocol.DecisionRunSandboxed,
	})
	if err != nil {
		t.Fatalf("prompt/approve with run_sandboxed: %v", err)
	}

	if len(ctrl.resolveCapCalls) != 1 {
		t.Fatalf("ResolveSandboxCapability calls = %d, want 1", len(ctrl.resolveCapCalls))
	}
	if ctrl.resolveCapCalls[0].action != sandboxauth.RunSandboxed {
		t.Fatalf("action = %q, want %q", ctrl.resolveCapCalls[0].action, sandboxauth.RunSandboxed)
	}

	srv.mu.Lock()
	pending := sess.pendingPrompt
	srv.mu.Unlock()
	if pending != nil {
		t.Fatal("pending prompt was not cleared")
	}
}

// TestRuntimeE2EOrdinaryApproval verifies that ordinary approvals still go
// through the legacy Approve path even over the new protocol pipe.
func TestRuntimeE2EOrdinaryApproval(t *testing.T) {
	workspace := t.TempDir()
	srv := New(Options{Workspace: workspace})
	ctrl := &capabilityTrackingController{fakeController: &fakeController{model: "local/test"}}
	target := srv.installTestSession(ctrl)

	srv.mu.Lock()
	sess := srv.sessions[target.SessionID]
	sess.pendingPrompt = &protocol.PendingPrompt{
		Kind: protocol.PromptApproval,
		Approval: &protocol.ApprovalPrompt{
			PromptID: "ord-1", Tool: "bash", Subject: "go test",
			AllowedDecisions: []protocol.PromptDecision{
				protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionDeny,
			},
		},
	}
	srv.mu.Unlock()

	hostSide, desktopSide := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go srv.serveConn(ctx, hostSide)

	wire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
		Name: "e2e-ord-test", StrictJSONRPC: true,
		MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	go func() { _ = wire.Serve(ctx) }()

	_, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: srv.buildID, ClientInstanceID: "e2e-ord-test", Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Send legacy deny via prompt/approve.
	_, err = wire.Request(ctx, string(protocol.MethodPromptApprove), protocol.PromptApproveParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "req-ord-1", ExpectedHostEpoch: srv.hostEpoch,
			Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		PromptID: "ord-1",
		Decision: protocol.DecisionDeny,
	})
	if err != nil {
		t.Fatalf("prompt/approve with deny: %v", err)
	}

	// Verify legacy Approve was called with allow=false.
	if len(ctrl.approveCalls) != 1 {
		t.Fatalf("Approve calls = %d, want 1; resolveCapCalls = %d", len(ctrl.approveCalls), len(ctrl.resolveCapCalls))
	}
	if ctrl.approveCalls[0].allow {
		t.Fatal("Approve allow = true, want false for deny")
	}
	if len(ctrl.resolveCapCalls) != 0 {
		t.Fatal("ResolveSandboxCapability should not be called for ordinary approval")
	}

	srv.mu.Lock()
	pending := sess.pendingPrompt
	srv.mu.Unlock()
	if pending != nil {
		t.Fatal("pending prompt was not cleared")
	}
}

// Serialization helpers for test output.
var (
	_ = sandboxauth.Action("")
)
