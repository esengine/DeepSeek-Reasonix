package boot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type failingDigestProvider struct{}

func (failingDigestProvider) Name() string { return "boot-failing-digest" }

func (failingDigestProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
		return nil, errors.New("upstream said no")
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat("work output line with detail. ", 400)}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type noticeSink struct {
	mu sync.Mutex
	ev []event.Event
}

func (s *noticeSink) Emit(e event.Event) { s.mu.Lock(); s.ev = append(s.ev, e); s.mu.Unlock() }

func (s *noticeSink) notice(code string) (event.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.ev {
		if e.Kind == event.Notice && e.Code == code {
			return e, true
		}
	}
	return event.Event{}, false
}

// A /compact whose summary request fails reaches the frontend as a typed notice:
// the code is what a frontend words in its own language, the English is only a
// fallback.
func TestEffectManualCompactFailureNoticeCarriesACode(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	kind := "boot-failing-digest-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return failingDigestProvider{}, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 200000
`)
	approveWorkspace(t, dir)
	sink := &noticeSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	for range 6 {
		_ = ctrl.Run(context.Background(), "keep going")
	}
	ctrl.Submit("/compact")
	var got event.Event
	var ok bool
	for range 100 {
		if got, ok = sink.notice(event.NoticeCodeCompactFailed); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		for _, e := range sink.ev {
			if e.Kind == event.Notice {
				t.Logf("notice %q %q %q", e.Code, e.Text, e.Detail)
			}
		}
		t.Fatal("no compact_failed notice reached the sink")
	}
	if got.Detail != "summary_failed" {
		t.Fatalf("notice detail = %q, want summary_failed", got.Detail)
	}
	if got.Text == "" {
		t.Fatal("notice lost its English fallback text")
	}
}
