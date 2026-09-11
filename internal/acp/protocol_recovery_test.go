package acp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
)

func TestProtocolRecoveryACPRejectsMissingAndUnsupportedToken(t *testing.T) {
	var attempts atomic.Int32
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { attempts.Add(1); return nil }}
	client, cleanup := startServer(t, factory)
	defer cleanup()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	response := client.call(t, "session/new", SessionNewParams{})
	var created SessionNewResult
	if err := json.Unmarshal(response.Result, &created); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "stale"} {
		response := client.callAsync("session/prompt", SessionPromptParams{SessionID: created.SessionID, Action: "protocol_recovery", RecoveryID: id})
		_, result := drainPrompt(t, client, response)
		want := ErrInvalidRequest
		if id == "" {
			want = ErrInvalidParams
		}
		if result.Error == nil || result.Error.Code != want {
			t.Fatalf("recovery error=%+v", result.Error)
		}
	}
	if attempts.Load() != 0 {
		t.Fatal("rejected recovery reached model runner")
	}
}
