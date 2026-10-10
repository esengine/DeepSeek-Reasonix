package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// unreachableSummarizer answers every summary request with a 400 and every
// other request with a short reply, so only the summary path is under test.
type unreachableSummarizer struct {
	mu        sync.Mutex
	summaries int
}

func (p *unreachableSummarizer) Name() string { return "boot-unreachable-summarizer" }

func (p *unreachableSummarizer) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if len(req.Tools) == 0 {
		p.mu.Lock()
		p.summaries++
		p.mu.Unlock()
		return nil, &provider.APIError{Provider: "relay", Status: 400, Body: `{"error":{"message":"summarizer unavailable"}}`}
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *unreachableSummarizer) attempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.summaries
}

// A due compaction whose retry is held after a failed summary reaches the
// frontend as a coded notice carrying the failure's code, once per turn.
func TestEffectHeldCompactionReachesTheFrontendCoded(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	prov := &unreachableSummarizer{}
	kind := "boot-unreachable-summarizer-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 20000
`)
	approveWorkspaceAt(t, home, dir)

	var mu sync.Mutex
	var held []event.Event
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeCompactHeld {
			mu.Lock()
			held = append(held, e)
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))

	for i := 0; prov.attempts() == 0; i++ {
		if i > 120 {
			t.Fatal("the summary was never attempted; the fixture never reached the trigger")
		}
		if err := ctrl.Run(context.Background(), "go "+strings.Repeat("log line ", 150)); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	mu.Lock()
	before := len(held)
	mu.Unlock()
	if before != 0 {
		t.Fatalf("%d held notices before any retry was held", before)
	}
	if err := ctrl.Run(context.Background(), "one more line"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(held) != 1 {
		t.Fatalf("held notices = %d, want 1", len(held))
	}
	if held[0].Detail != "summary_failed" || held[0].Level != event.LevelWarn {
		t.Fatalf("held notice = %+v, want the failure code as detail at warn level", held[0])
	}
}
