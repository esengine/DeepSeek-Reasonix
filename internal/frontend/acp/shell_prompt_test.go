package acp

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/permission"
)

// A `!` prompt is the user's own command: it runs as the user typed it,
// streams back as a tool call, and the model answers its output before the
// prompt ends.
func TestE2EShellPromptRunsTheUsersCommand(t *testing.T) {
	prov := &scriptedProvider{name: "fake", responses: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "the marker printed"},
		{Type: provider.ChunkDone},
	}}}
	factory := &e2eFactory{
		prov:       prov,
		tool:       fakeTool{name: "peek", ro: true, out: "unused"},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: testenv.TempDir(t),
	}
	client, stop := startServer(t, factory)
	defer stop()

	sid := openSession(t, client)
	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: "!echo acp-shell-marker"}},
	})
	notifs, resp := drainPrompt(t, client, promptCh)

	var output strings.Builder
	for _, n := range notifs {
		if updateKind(t, n) != "tool_call_update" {
			continue
		}
		var p struct {
			Update struct {
				Content []struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"content"`
			} `json:"update"`
		}
		_ = json.Unmarshal(n.Params, &p)
		for _, c := range p.Update.Content {
			output.WriteString(c.Content.Text)
		}
	}
	if !strings.Contains(output.String(), "acp-shell-marker") {
		t.Fatalf("shell output did not reach the editor: %q", output.String())
	}
	var pr SessionPromptResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil || pr.StopReason != StopEndTurn {
		t.Fatalf("prompt result = %+v, %v", pr, err)
	}
	prov.mu.Lock()
	n := prov.calls
	prov.mu.Unlock()
	if n != 1 {
		t.Fatalf("the model answered the user's command %d times, want once", n)
	}
}
