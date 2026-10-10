package boot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const thinkFor = 60 * time.Millisecond

// thinkingProvider reasons, pauses, then answers, so the thought has a length.
type thinkingProvider struct{}

func (thinkingProvider) Name() string { return "boot-thinking" }

func (thinkingProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	go func() {
		defer close(ch)
		ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: "weighing it"}
		time.Sleep(thinkFor)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "answer"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

// How long a reply thought is measured by the host and kept with the turn, so
// the live frame and a reopened transcript report the same figure.
func TestEffectThinkingTimeIsKeptWithTheTurn(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	provider.Register("boot-thinking", func(provider.Config) (provider.Provider, error) { return thinkingProvider{}, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-thinking"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	var mu sync.Mutex
	var framed int64
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Message {
			mu.Lock()
			framed = e.ThoughtMs
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "think, then answer"); err != nil && !strings.Contains(err.Error(), "readiness") {
		t.Fatalf("Run: %v", err)
	}
	var kept int64
	for _, m := range ctrl.History() {
		if m.Role == provider.RoleAssistant && m.Content == "answer" {
			kept = m.ThoughtMs
		}
	}
	mu.Lock()
	defer mu.Unlock()
	floor := thinkFor.Milliseconds() - 10
	if kept < floor || framed < floor {
		t.Fatalf("thought kept %d ms, framed %d ms; want both at least %d", kept, framed, floor)
	}
	if kept != framed {
		t.Fatalf("the transcript keeps %d ms but the live frame said %d ms", kept, framed)
	}
}
