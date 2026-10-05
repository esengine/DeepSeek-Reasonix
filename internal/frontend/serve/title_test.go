package serve

import (
	"context"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// Generation runs on the filler's goroutines, so the recorder is shared.
type recordingTitleProvider struct {
	mu       sync.Mutex
	requests []provider.Request
}

func (p *recordingTitleProvider) Name() string { return "recording-title" }

func (p *recordingTitleProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: req.Messages[len(req.Messages)-1].Content}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *recordingTitleProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *recordingTitleProvider) at(i int) provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[i]
}

// waitTitle blocks until the scheduled generation has reached the cache.
func waitTitle(t *testing.T, s *Server, name, source string, mod int64) string {
	t.Helper()
	for range 200 {
		if got, ok := s.titles.get(name, source, mod); ok {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("title for %q never reached the cache", name)
	return ""
}

func TestTitleProviderDisablesReasoning(t *testing.T) {
	cfg := titleProviderConfig(&config.ProviderEntry{
		Name:    "deepseek-flash",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
	})
	if got := cfg.Extra["effort"]; got != "disabled" {
		t.Fatalf("title provider effort = %v, want disabled", got)
	}
}

func TestGenerateTitleStripsPasteLabelAndUsesShortBudget(t *testing.T) {
	prov := &recordingTitleProvider{}
	s := &Server{titleProv: prov}
	got := s.generateTitle(context.Background(), "[已粘贴文本 #1 · 20 行]\nfix the login loop")
	if got != "fix the login loop" {
		t.Fatalf("title = %q, want pasted label removed", got)
	}
	if prov.count() != 1 {
		t.Fatalf("requests = %d, want 1", prov.count())
	}
	req := prov.at(0)
	if req.MaxTokens != 60 {
		t.Fatalf("MaxTokens = %d, want 60", req.MaxTokens)
	}
	if req.Messages[0].Content != titlePrompt || req.Messages[1].Content != "fix the login loop" {
		t.Fatalf("title messages = %+v", req.Messages)
	}
}

func TestSessionTitleCachesByFirstMessageAcrossMtimeChanges(t *testing.T) {
	dir := testenv.TempDir(t)
	prov := &recordingTitleProvider{}
	s := &Server{titleProv: prov, titles: newTitleCache(dir), fill: newTitleFiller()}

	if got := s.sessionTitle("a.jsonl", "first prompt", 100); got != "first prompt" {
		t.Fatalf("first title = %q", got)
	}
	waitTitle(t, s, "a.jsonl", "first prompt", 100)

	if got := s.sessionTitle("a.jsonl", "first prompt", 200); got != "first prompt" {
		t.Fatalf("title after append = %q", got)
	}
	if prov.count() != 1 {
		t.Fatalf("requests after mtime-only change = %d, want 1", prov.count())
	}

	if got := s.sessionTitle("a.jsonl", "replacement prompt", 300); got != "replacement prompt" {
		t.Fatalf("title after replacing first turn = %q", got)
	}
	waitTitle(t, s, "a.jsonl", "replacement prompt", 300)
	if prov.count() != 2 {
		t.Fatalf("requests after replacing first turn = %d, want 2", prov.count())
	}

	freshProv := &recordingTitleProvider{}
	fresh := &Server{titleProv: freshProv, titles: newTitleCache(dir), fill: newTitleFiller()}
	if got := fresh.sessionTitle("a.jsonl", "replacement prompt", 400); got != "replacement prompt" {
		t.Fatalf("persisted title = %q", got)
	}
	if freshProv.count() != 0 {
		t.Fatalf("fresh server regenerated persisted title %d time(s)", freshProv.count())
	}
}

// The list request is the thing that used to block; it must now return without
// waiting on the provider at all.
func TestSessionTitleDoesNotBlockOnGeneration(t *testing.T) {
	release := make(chan struct{})
	prov := &blockingTitleProvider{release: release}
	s := &Server{titleProv: prov, titles: newTitleCache(testenv.TempDir(t)), fill: newTitleFiller()}

	done := make(chan string, 1)
	go func() { done <- s.sessionTitle("a.jsonl", "slow prompt", 100) }()

	select {
	case got := <-done:
		if got != "slow prompt" {
			t.Fatalf("title = %q, want the preview", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sessionTitle blocked on the provider")
	}
	close(release)
}

type blockingTitleProvider struct{ release chan struct{} }

func (p *blockingTitleProvider) Name() string { return "blocking-title" }

func (p *blockingTitleProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	<-p.release
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func TestPreviewTitleStripsOnlyLeadingPasteLabel(t *testing.T) {
	if got := previewTitle("[Pasted text #2 · 42 lines]\nfunc foo() { return 1 }"); got != "func foo() { return 1 }" {
		t.Fatalf("previewTitle = %q", got)
	}
	const inline = "Explain [Pasted text #2 · 42 lines] handling"
	if got := previewTitle(inline); got != inline {
		t.Fatalf("inline label changed to %q", got)
	}
}
