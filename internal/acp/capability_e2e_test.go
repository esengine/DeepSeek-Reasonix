package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/sandboxauth"
)

// TestE2ESandboxCapabilityApprovalRoundTrip exercises the sandbox capability
// approval path through a real ACP protocol connection (io.Pipe + Conn).
// Verifies that a sandbox_capability ApprovalRequest emitted through
// the updateSink produces the correct session/request_permission on the wire,
// and that a typed action response resolves the capability correctly.
func TestE2ESandboxCapabilityApprovalRoundTrip(t *testing.T) {
	// ── Two Pipe pairs for bidirectional communication ──
	serverToClientR, serverToClientW := io.Pipe()
	clientToServerR, clientToServerW := io.Pipe()

	// Client-side: decode NDJSON frames from the server.
	dec := json.NewDecoder(serverToClientR)
	frames := make(chan wireFrame, 16)
	go func() {
		for {
			var f wireFrame
			if err := dec.Decode(&f); err != nil {
				return
			}
			frames <- f
		}
	}()

	// Client-side writer.
	enc := json.NewEncoder(clientToServerW)

	// Server-side: real ACP Conn + updateSink.
	serverConn := NewConn(clientToServerR, serverToClientW)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = serverConn.Serve(ctx) }()

	sink := newUpdateSink(serverConn, "sess-cap-e2e")
	resolved := make(chan sandboxauth.Action, 1)
	sink.bindResolveCapability(func(id string, action sandboxauth.Action) {
		resolved <- action
	})

	// Emit a sandbox_capability approval event.
	sink.Emit(event.Event{
		Kind: event.ApprovalRequest,
		Approval: event.Approval{
			ID: "cap-e2e-1", Tool: "bash", Subject: "pip install requests",
			Kind: sandboxauth.ApprovalKind,
			SandboxCapability: &sandboxauth.Prompt{
				CanonicalExecutable: "/usr/bin/pip",
				Argv:                []string{"pip", "install", "requests"},
				Reusable:            true,
			},
		},
	})

	// Wait for the session/request_permission request from the wire.
	timeout := time.After(5 * time.Second)
	var reqFrame wireFrame
	select {
	case f := <-frames:
		if f.Method != "" && f.ID != nil {
			reqFrame = f
		} else {
			// First wireFrame may be a notification; try the next.
			select {
			case f2 := <-frames:
				reqFrame = f2
			case <-timeout:
				t.Fatal("timed out waiting for request_permission request")
			}
		}
	case <-timeout:
		t.Fatal("timed out waiting for request_permission request")
	}
	if reqFrame.Method != "session/request_permission" {
		t.Fatalf("request method = %q, want session/request_permission", reqFrame.Method)
	}

	// Verify the request params have sandbox_capability toolCall kind.
	var req struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			Kind       string `json:"kind"`
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(reqFrame.Params, &req); err != nil {
		t.Fatalf("unmarshal request params: %v", err)
	}
	if req.SessionID != "sess-cap-e2e" {
		t.Errorf("sessionId = %q, want sess-cap-e2e", req.SessionID)
	}
	if req.ToolCall.Kind != "sandbox_capability" {
		t.Errorf("toolCall.kind = %q, want sandbox_capability", req.ToolCall.Kind)
	}
	if req.ToolCall.ToolCallID != "gate-cap-e2e-1" {
		t.Errorf("toolCallId = %q, want gate-cap-e2e-1", req.ToolCall.ToolCallID)
	}
	if !strings.Contains(req.ToolCall.Title, "pip install requests") {
		t.Errorf("toolCall.title = %q, want it to contain 'pip install requests'", req.ToolCall.Title)
	}

	// Verify option kinds match the ACP v1 schema: all options should use
	// standard PermissionOptionKind values.
	validKinds := map[string]bool{"allow_once": true, "allow_always": true, "reject_once": true, "reject_always": true, "allow_persistent": true}
	for _, opt := range req.Options {
		if !validKinds[opt.Kind] {
			t.Errorf("option %q has non-standard kind %q", opt.OptionID, opt.Kind)
		}
	}

	// Verify the correct decisions are offered for a reusable capability.
	var foundOnce, foundSession, foundPersistent, foundRun, foundCancel bool
	for _, opt := range req.Options {
		switch opt.OptionID {
		case string(sandboxauth.AllowOnce):
			foundOnce = true
		case string(sandboxauth.AllowSession):
			foundSession = true
		case string(sandboxauth.AllowPersistent):
			foundPersistent = true
		case string(sandboxauth.RunSandboxed):
			foundRun = true
		case string(sandboxauth.CancelCommand):
			foundCancel = true
		}
	}
	if !foundOnce {
		t.Error("options missing allow_once")
	}
	if !foundSession {
		t.Error("reusable=true: options missing allow_session")
	}
	if !foundPersistent {
		t.Error("reusable=true: options missing allow_persistent")
	}
	if !foundRun {
		t.Error("options missing run_sandboxed")
	}
	if !foundCancel {
		t.Error("options missing cancel_command")
	}

	// Client responds with allow_once.
	resp := makePermissionResult("selected", string(sandboxauth.AllowOnce))
	rawResp, _ := json.Marshal(resp)
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqFrame.ID,
		"result":  json.RawMessage(rawResp),
	}
	if err := enc.Encode(response); err != nil {
		t.Fatalf("encode response: %v", err)
	}

	// Verify the capability was resolved with AllowOnce.
	select {
	case action := <-resolved:
		if action != sandboxauth.AllowOnce {
			t.Errorf("resolved action = %q, want allow_once", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolveCapability was never called")
	}
}

// TestE2ESandboxCapabilityDenied exercises the path where the client rejects
// or fails to respond, verifying the safe fallback to RunSandboxed.
func TestE2ESandboxCapabilityDenied(t *testing.T) {
	tests := []struct {
		name       string
		sendErr    bool // if true, send a JSON-RPC error instead of a result
		wantAction sandboxauth.Action
	}{
		{name: "cancelled", wantAction: sandboxauth.RunSandboxed},
		{name: "transport error", sendErr: true, wantAction: sandboxauth.RunSandboxed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverToClientR, serverToClientW := io.Pipe()
			clientToServerR, clientToServerW := io.Pipe()

			dec := json.NewDecoder(serverToClientR)
			frames := make(chan wireFrame, 16)
			go func() {
				for {
					var f wireFrame
					if err := dec.Decode(&f); err != nil {
						return
					}
					frames <- f
				}
			}()
			enc := json.NewEncoder(clientToServerW)

			serverConn := NewConn(clientToServerR, serverToClientW)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = serverConn.Serve(ctx) }()

			sink := newUpdateSink(serverConn, "sess-cap-deny")
			resolved := make(chan sandboxauth.Action, 1)
			sink.bindResolveCapability(func(id string, action sandboxauth.Action) {
				resolved <- action
			})

			sink.Emit(event.Event{
				Kind: event.ApprovalRequest,
				Approval: event.Approval{
					ID: "cap-deny-1", Tool: "bash", Subject: "rm -rf /", Kind: sandboxauth.ApprovalKind,
					SandboxCapability: &sandboxauth.Prompt{Argv: []string{"rm", "-rf", "/"}, Reusable: false},
				},
			})

			// Wait for the request_permission request.
			timeout := time.After(5 * time.Second)
			var reqFrame wireFrame
			select {
			case f := <-frames:
				if f.Method == "session/request_permission" && f.ID != nil {
					reqFrame = f
				} else {
					select {
					case f2 := <-frames:
						reqFrame = f2
					case <-timeout:
						t.Fatal("timed out waiting for request_permission")
					}
				}
			case <-timeout:
				t.Fatal("timed out waiting for request_permission")
			}

			if tt.sendErr {
				// Send a JSON-RPC error response.
				response := map[string]any{
					"jsonrpc": "2.0",
					"id":      reqFrame.ID,
					"error": map[string]any{
						"code":    -32000,
						"message": "transport error",
					},
				}
				enc.Encode(response)
			} else {
				// Send "cancelled" outcome.
				resp := makePermissionResult("cancelled", "")
				rawResp, _ := json.Marshal(resp)
				response := map[string]any{
					"jsonrpc": "2.0",
					"id":      reqFrame.ID,
					"result":  json.RawMessage(rawResp),
				}
				enc.Encode(response)
			}

			select {
			case action := <-resolved:
				if action != tt.wantAction {
					t.Errorf("action = %q, want %q", action, tt.wantAction)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("resolveCapability was never called")
			}
		})
	}
}

// wireFrame is a minimal JSON-RPC 2.0 wireFrame used for decoding server output.
type wireFrame struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *json.RawMessage `json:"error,omitempty"`
}

// permissionResult builds a PermissionRequestResult for encoding.
type permissionResult struct {
	Outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId,omitempty"`
	} `json:"outcome"`
}

func makePermissionResult(outcome, optionID string) permissionResult {
	var r permissionResult
	r.Outcome.Outcome = outcome
	r.Outcome.OptionID = optionID
	return r
}
