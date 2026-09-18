package boot

// A truncating provider answers HTTP 200 with no error field, so the overflow
// ladder, which needs a parseable 400/413/422, never sees it. These assert the
// replacement signal through the real Build stack.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// truncatingProvider reports a fixed prompt size however much is sent, which is
// what a server serving a smaller window than it was asked for looks like.
type truncatingProvider struct {
	mu           sync.Mutex
	promptTokens int
	honest       bool
	calls        int
}

func (p *truncatingProvider) Name() string { return "boot-truncation-test" }

func (p *truncatingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	reported := p.promptTokens
	if p.honest {
		chars := 0
		for _, m := range req.Messages {
			chars += len(m.Content)
		}
		for _, tool := range req.Tools {
			chars += len(tool.Name) + len(tool.Description) + len(tool.Parameters)
		}
		reported = max(1, chars/4)
	}
	p.mu.Unlock()

	chunks := []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens: reported, CompletionTokens: 1,
			TotalTokens: reported + 1, RequestCount: 1,
		}},
		{Type: provider.ChunkDone},
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

type noticeSink struct {
	mu      sync.Mutex
	notices []event.Event
}

func (s *noticeSink) Emit(e event.Event) {
	if e.Kind != event.Notice {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = append(s.notices, e)
}

func (s *noticeSink) truncationNotices() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.notices {
		if e.Code == event.NoticeCodePromptTruncatedByServer {
			out = append(out, e)
		}
	}
	return out
}

// truncationRun drives two turns with prompts big enough to clear the detector's
// minimum, and returns the notices the frontend sink actually received.
func truncationRun(t *testing.T, kind string, p *truncatingProvider) *noticeSink {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 200000
`)

	sink := &noticeSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	big := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 1500)
	for range 2 {
		if err := ctrl.Run(context.Background(), big); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if p.calls == 0 {
		t.Fatal("no request reached the provider boundary")
	}
	return sink
}

// The defect: the user is told nothing while the model silently loses its tool
// definitions. One warning, and only one, however many turns run.
func TestEffectSilentTruncationWarnsExactlyOnce(t *testing.T) {
	p := &truncatingProvider{promptTokens: 2051}
	got := truncationRun(t, "truncation-effect-warn", p).truncationNotices()
	if len(got) != 1 {
		t.Fatalf("got %d truncation notices across two turns, want exactly 1", len(got))
	}
	if got[0].Level != event.LevelWarn {
		t.Errorf("notice level = %v, want LevelWarn: nothing was recovered", got[0].Level)
	}
	if !strings.Contains(got[0].Detail, "2051") {
		t.Errorf("notice detail %q does not name the accepted prompt size", got[0].Detail)
	}
}

// The no-false-positive guard: an honest provider must stay silent, or the
// warning is noise on every healthy session.
func TestEffectHonestProviderNeverWarns(t *testing.T) {
	p := &truncatingProvider{honest: true}
	if got := truncationRun(t, "truncation-effect-honest", p).truncationNotices(); len(got) != 0 {
		t.Fatalf("an honest provider produced %d truncation notices: %+v", len(got), got)
	}
}
