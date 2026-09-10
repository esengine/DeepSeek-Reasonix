package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestMCPInteractionRequiresVersionedOptIn(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{}`, false},
		{`{"_meta":{"reasonix.io":{"mcpInteraction":true}}}`, false},
		{`{"_meta":{"reasonix.io":{"mcpInteraction":{"supported":true}}}}`, false},
		{`{"_meta":{"reasonix.io":{"mcpInteraction":{"supported":true,"schemaVersion":2}}}}`, false},
		{`{"_meta":{"reasonix.io":{"mcpInteraction":{"supported":true,"schemaVersion":1}}}}`, true},
	} {
		var caps ClientCapabilities
		if err := json.Unmarshal([]byte(tc.raw), &caps); err != nil {
			t.Fatal(err)
		}
		if got := clientMCPInteractionSupported(caps); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.raw, got, tc.want)
		}
		svc := &service{clientCaps: caps}
		var params SessionParams
		svc.bindClientIO(&params, "session")
		if params.MCPInteractions != tc.want {
			t.Fatalf("factory opt-in not propagated: %+v", params)
		}
	}
}

func TestMCPInteractionRoundTripAndInvalidReplies(t *testing.T) {
	for _, tc := range []struct {
		name, response, action string
		supported              bool
	}{
		{"accept", `{"action":"accept","content":{"answer":"yes"}}`, "accept", true},
		{"decline", `{"action":"decline","content":{"answer":"discard"}}`, "decline", true},
		{"unknown", `{"action":"allow"}`, "cancel", true},
		{"malformed", `{"action":`, "cancel", true},
		{"legacy", `{"action":"accept"}`, "cancel", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan MCPInteractionParams, 1)
			n := &fakeNotifier{onReq: func(method string, params any) (json.RawMessage, error) {
				if method != mcpInteractionMethod {
					t.Errorf("method = %s", method)
				}
				seen <- params.(MCPInteractionParams)
				return json.RawMessage(tc.response), nil
			}}
			sink := newUpdateSink(n, "session-one")
			resolved := make(chan MCPInteractionResult, 1)
			sink.bindMCPInteraction(tc.supported, func(id, action string, content map[string]any) error {
				if id != "prompt-1" {
					t.Errorf("prompt = %s", id)
				}
				resolved <- MCPInteractionResult{Action: action, Content: content}
				return nil
			})
			sink.Emit(event.Event{Kind: event.MCPInteractionRequest, MCPInteraction: event.MCPInteraction{ID: "prompt-1", TurnID: "turn-1", Server: "browser", Mode: "form", Message: "Confirm", RequestedSchema: json.RawMessage(`{"type":"object"}`)}})
			select {
			case got := <-resolved:
				if got.Action != tc.action {
					t.Fatalf("action = %s", got.Action)
				}
				if got.Action != "accept" && got.Content != nil {
					t.Fatal("non-accept response retained form values")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("MCP prompt was not resolved")
			}
			if tc.supported {
				got := <-seen
				if got.SessionID != "session-one" || got.TurnID != "turn-1" || got.PromptID != "prompt-1" {
					t.Fatalf("routing identity lost: %+v", got)
				}
			} else {
				select {
				case <-seen:
					t.Fatal("legacy client received vendor request")
				default:
				}
			}
		})
	}
}

func TestMCPInteractionLateReplyCannotResolveReplacementController(t *testing.T) {
	requested, release := make(chan struct{}), make(chan struct{})
	n := &fakeNotifier{onReqCtx: func(context.Context, string, any) (json.RawMessage, error) {
		close(requested)
		<-release
		return json.RawMessage(`{"action":"accept"}`), nil
	}}
	sink := newUpdateSink(n, "session-one")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink.setTurnContext(ctx)
	oldResult := make(chan string, 1)
	replacementResult := make(chan string, 1)
	sink.bindMCPInteraction(true, func(_ string, action string, _ map[string]any) error { oldResult <- action; return nil })
	sink.Emit(event.Event{Kind: event.MCPInteractionRequest, MCPInteraction: event.MCPInteraction{ID: "1", Mode: "form"}})
	<-requested
	cancel()
	sink.bindMCPInteraction(true, func(_ string, action string, _ map[string]any) error { replacementResult <- action; return nil })
	close(release)
	select {
	case action := <-oldResult:
		if action != "cancel" {
			t.Fatalf("cancelled request accepted: %s", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old request unresolved")
	}
	select {
	case <-replacementResult:
		t.Fatal("late response reached replacement controller")
	default:
	}
}

func TestMCPInteractionRefusesUnsafeURLWithoutClientRequest(t *testing.T) {
	n := &fakeNotifier{onReq: func(string, any) (json.RawMessage, error) { t.Error("unsafe URL forwarded"); return nil, nil }}
	sink := newUpdateSink(n, "session-one")
	resolved := make(chan string, 1)
	sink.bindMCPInteraction(true, func(_ string, action string, _ map[string]any) error { resolved <- action; return nil })
	sink.Emit(event.Event{Kind: event.MCPInteractionRequest, MCPInteraction: event.MCPInteraction{ID: "1", Mode: "url", URL: "https://user:secret@example.invalid/"}})
	select {
	case action := <-resolved:
		if action != "cancel" {
			t.Fatal(action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unsafe URL prompt hung")
	}
}
