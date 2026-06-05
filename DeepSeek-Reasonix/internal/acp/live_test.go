//go:build live

// Live network end-to-end test, excluded from the normal suite by the `live`
// build tag. Run it against a real model with:
//
//	set -a; . /path/to/.env; set +a
//	go test -tags live -run Live ./internal/acp/ -v
//
// It drives the full ACP stack — acp.Serve → control.Controller → agent.Agent →
// the real OpenAI-compatible provider — over a tiny prompt, proving the live
// model path the hermetic tests stub out.
package acp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/openai" // registers the "openai" provider kind
	"reasonix/internal/tool"
)

type liveFactory struct{ prov provider.Provider }

func (f *liveFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	executor := agent.New(f.prov, tool.NewRegistry(),
		agent.NewSession("You are a terse assistant. Answer in as few words as possible."),
		agent.Options{MaxSteps: 3}, p.Sink)
	return control.New(control.Options{Runner: executor, Executor: executor, Sink: p.Sink, Label: "deepseek"}), nil
}

func TestLiveDeepSeekPrompt(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	prov, err := provider.New("openai", provider.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
		APIKey:  key,
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}

	client, stop := startServer(t, &liveFactory{prov: prov})
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	resp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	if err := json.Unmarshal(resp.Result, &nr); err != nil {
		t.Fatalf("session/new: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "Reply with exactly one word: hi"}},
	})

	// Collect updates until the prompt response arrives (network: allow up to 60s).
	var notifs []frame
	var pResp frame
	deadline := time.After(60 * time.Second)
collect:
	for {
		select {
		case f := <-client.notifs:
			notifs = append(notifs, f)
		case pResp = <-promptCh:
			break collect
		case <-deadline:
			t.Fatal("live prompt timed out after 60s")
		}
	}

	var text string
	for _, n := range notifs {
		if updateKind(t, n) != "agent_message_chunk" {
			continue
		}
		var p struct {
			Update struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		json.Unmarshal(n.Params, &p)
		text += p.Update.Content.Text
	}
	if text == "" {
		t.Fatal("no agent_message_chunk text received from the live model")
	}

	var pr SessionPromptResult
	if err := json.Unmarshal(pResp.Result, &pr); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if pr.StopReason != StopEndTurn {
		t.Errorf("stopReason = %q, want end_turn", pr.StopReason)
	}
	t.Logf("live model replied: %q (stopReason=%s)", text, pr.StopReason)
}
