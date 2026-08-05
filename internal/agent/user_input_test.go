package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type userInputCaptureProvider struct {
	request provider.Request
}

func (p *userInputCaptureProvider) Name() string { return "capture" }

func (p *userInputCaptureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.request = req
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
}

func TestRunPersistsRawUserInputSeparatelyFromProviderContext(t *testing.T) {
	prov := &userInputCaptureProvider{}
	sess := NewSession("system")
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)

	const raw = "fix the bug"
	const composed = "<capability-route version=\"1\">\nuse review\n</capability-route>\n\nfix the bug"
	ctx := WithRawUserInput(context.Background(), raw)
	if err := a.Run(ctx, composed); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stored := sess.Snapshot()
	if len(stored) < 2 {
		t.Fatalf("stored messages = %d, want system and user", len(stored))
	}
	if got := stored[1].Content; got != composed {
		t.Fatalf("stored provider content = %q, want composed %q", got, composed)
	}
	if got := stored[1].RawContent; got != raw {
		t.Fatalf("stored raw content = %q, want raw %q", got, raw)
	}
	if stored[1].ProviderContent != "" {
		t.Fatalf("stored transitional provider content was not cleared: %+v", stored[1])
	}
	if len(prov.request.Messages) < 2 || prov.request.Messages[1].Content != composed {
		t.Fatalf("provider request did not receive composed context: %+v", prov.request.Messages)
	}
	if prov.request.Messages[1].RawContent != "" || prov.request.Messages[1].ProviderContent != "" {
		t.Fatalf("provider request leaked display metadata: %+v", prov.request.Messages[1])
	}

	encoded, err := json.Marshal(stored[1])
	if err != nil {
		t.Fatalf("marshal stored user turn: %v", err)
	}
	var legacy struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatalf("decode with previous-release shape: %v", err)
	}
	if legacy.Content != composed {
		t.Fatalf("previous-release reader sees %q, want provider-visible %q", legacy.Content, composed)
	}
}
