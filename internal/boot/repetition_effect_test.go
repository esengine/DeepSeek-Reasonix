package boot

// Final-boundary proof for the client repetition guard: a degenerate stream is
// discarded and re-sampled through the real Build stack, and the degenerate
// text never reaches the provider on the following turn.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type repetitionEffectProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *repetitionEffectProvider) Name() string { return "boot-repetition-effect" }

func (p *repetitionEffectProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	call := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		if call == 1 {
			for {
				select {
				case <-ctx.Done():
					return
				case ch <- provider.Chunk{Type: provider.ChunkText, Text: "Let me run the gate scripts now, for real this time. "}:
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case ch <- provider.Chunk{Type: provider.ChunkText, Text: "gates pass"}:
		}
		select {
		case <-ctx.Done():
			return
		case ch <- provider.Chunk{Type: provider.ChunkDone}:
		}
	}()
	return ch, nil
}

func (p *repetitionEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func TestEffectRepetitionGuardDiscardsDegenerateAttempt(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &repetitionEffectProvider{}
	provider.Register("boot-repetition-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-repetition-effect"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "check the gates"); err != nil {
		t.Fatalf("Run: %v (the guard should discard the degenerate attempt and commit the retry)", err)
	}
	if err := ctrl.Run(context.Background(), "and then?"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 3 {
		t.Fatalf("provider requests = %d, want 3 (degenerate + clean retry + next turn)", len(reqs))
	}
	for _, m := range reqs[2].Messages {
		if strings.Contains(m.Content, "Let me run the gate scripts") {
			t.Fatalf("degenerate attempt reached the provider on the next turn: %q", m.Content)
		}
	}
	var sawClean bool
	for _, m := range reqs[2].Messages {
		if m.Role == provider.RoleAssistant && strings.Contains(m.Content, "gates pass") {
			sawClean = true
		}
	}
	if !sawClean {
		t.Fatal("clean retry text missing from the next turn's provider request")
	}
}
