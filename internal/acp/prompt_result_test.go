package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestServePromptErrorReturnsJSONRPCError(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz"
	const opaqueSecret = "relayKeyAbcdefghijkl"
	const maskedSuffix = "ae54"
	var mu sync.Mutex
	attempts := 0
	factory := &fakeFactory{behavior: func(_ context.Context, sink event.Sink, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return errors.New("provider failed: Authorization: Bearer " + secret + " credential " + opaqueSecret + " rejected token ****" + maskedSuffix + "\ndetails=" + strings.Repeat("x", 3_000))
		}
		sink.Emit(event.Event{Kind: event.Text, Text: "recovered"})
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "fail"}},
	})
	notifications, resp := drainPrompt(t, client, promptCh)
	if resp.Error == nil {
		t.Fatalf("prompt response = %+v, want JSON-RPC error", resp)
	}
	if resp.Error.Code != ErrInternal {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrInternal)
	}
	if !strings.HasPrefix(resp.Error.Message, "session/prompt: provider failed:") {
		t.Errorf("error message = %q, want underlying cause", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, secret) || strings.Contains(resp.Error.Message, opaqueSecret) || strings.Contains(resp.Error.Message, maskedSuffix) {
		t.Errorf("error message leaked credential: %q", resp.Error.Message)
	}
	if len(resp.Error.Message) > len("session/prompt: ")+2_048 {
		t.Errorf("error message length = %d, want at most %d", len(resp.Error.Message), len("session/prompt: ")+2_048)
	}
	if len(resp.Result) != 0 {
		t.Errorf("result = %s, want no stopReason result on failure", resp.Result)
	}

	wantReason := strings.TrimPrefix(resp.Error.Message, "session/prompt: ")
	foundStatus := false
	for _, notification := range notifications {
		if notification.Method != sessionStatusUpdateMethod {
			continue
		}
		var update ReasonixStatusUpdate
		if err := json.Unmarshal(notification.Params, &update); err != nil {
			t.Fatalf("status update: %v", err)
		}
		if update.Event == "error" {
			foundStatus = true
			if update.Status.TurnOutcome.Kind != "error" || update.Status.TurnOutcome.Reason != wantReason {
				t.Errorf("error status = %+v, want reason %q", update.Status.TurnOutcome, wantReason)
			}
		}
	}
	if !foundStatus {
		t.Fatal("missing error status update before prompt response")
	}

	retryCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "retry"}},
	})
	_, retryResp := drainPrompt(t, client, retryCh)
	if retryResp.Error != nil {
		t.Fatalf("retry prompt errored: %+v", retryResp.Error)
	}
	var retryResult SessionPromptResult
	if err := json.Unmarshal(retryResp.Result, &retryResult); err != nil {
		t.Fatalf("retry prompt result: %v", err)
	}
	if retryResult.StopReason != StopEndTurn {
		t.Errorf("retry stopReason = %q, want %q", retryResult.StopReason, StopEndTurn)
	}
}

func TestServePromptRecoveryPauseReturnsEndTurn(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error {
		return &agent.RecoveryPauseError{Message: "automatic recovery paused"}
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "pause"}},
	})
	_, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("prompt error = %+v, want controlled completion", resp.Error)
	}
	var result SessionPromptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if result.StopReason != StopEndTurn {
		t.Errorf("stopReason = %q, want %q", result.StopReason, StopEndTurn)
	}
}

func TestServeCancelWhenRunnerReturnsNil(t *testing.T) {
	started := make(chan struct{})
	factory := &fakeFactory{behavior: func(ctx context.Context, _ event.Sink, _ string) error {
		close(started)
		<-ctx.Done()
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "loop"}},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt never started")
	}
	client.notify("session/cancel", SessionCancelParams{SessionID: nr.SessionID})

	_, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("cancelled prompt errored: %+v", resp.Error)
	}
	var result SessionPromptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if result.StopReason != StopCancelled {
		t.Errorf("stopReason = %q, want %q", result.StopReason, StopCancelled)
	}
}
