package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestServePromptReadinessGateFailureEndsTurnWithWarning(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz"
	const opaqueSecret = "readinessSecretAbc123"
	longReason := "missing verification; Authorization: Bearer " + secret +
		" credential " + opaqueSecret + "; details=" + strings.Repeat("x", 3_000)
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error {
		return fmt.Errorf("turn stopped: %w", &agent.FinalReadinessError{Attempts: 1, Reason: longReason, Missing: []string{"verify"}})
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
		Prompt:    []ContentBlock{{Type: "text", Text: "do the thing"}},
	})
	notifs, resp := drainPrompt(t, client, promptCh)
	var pr SessionPromptResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if pr.StopReason != StopEndTurn {
		t.Fatalf("stopReason = %q, want end_turn", pr.StopReason)
	}

	warned := false
	for _, n := range notifs {
		var params SessionUpdateParams
		if err := json.Unmarshal(n.Params, &params); err != nil {
			continue
		}
		upd, ok := params.Update.(map[string]any)
		if !ok || upd["sessionUpdate"] != "agent_message_chunk" {
			continue
		}
		content, _ := upd["content"].(map[string]any)
		text, _ := content["text"].(string)
		if strings.Contains(text, "[warning]") && strings.Contains(text, "missing verification") {
			if strings.Contains(text, secret) || strings.Contains(text, opaqueSecret) {
				t.Fatalf("readiness warning leaked credential: %q", text)
			}
			if len(text) > len("\n\n[warning] ")+2_048 {
				t.Fatalf("readiness warning length = %d, want at most %d", len(text), len("\n\n[warning] ")+2_048)
			}
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no readiness warning chunk in notifications: %+v", notifs)
	}
}
