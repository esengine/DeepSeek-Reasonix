package serve

import (
	"context"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

type recordingTitleProvider struct {
	requests []provider.Request
}

func (p *recordingTitleProvider) Name() string { return "recording-title" }

func (p *recordingTitleProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: req.Messages[len(req.Messages)-1].Content}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
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
	if len(prov.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.MaxTokens != 60 {
		t.Fatalf("MaxTokens = %d, want 60", req.MaxTokens)
	}
	if req.Messages[0].Content != titlePrompt || req.Messages[1].Content != "fix the login loop" {
		t.Fatalf("title messages = %+v", req.Messages)
	}
}

func TestSessionTitleCachesByFirstMessageAcrossMtimeChanges(t *testing.T) {
	dir := t.TempDir()
	prov := &recordingTitleProvider{}
	s := &Server{titleProv: prov, titles: newTitleCache(dir)}

	if got := s.sessionTitle(context.Background(), "a.jsonl", "first prompt", 100); got != "first prompt" {
		t.Fatalf("first title = %q", got)
	}
	if got := s.sessionTitle(context.Background(), "a.jsonl", "first prompt", 200); got != "first prompt" {
		t.Fatalf("title after append = %q", got)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests after mtime-only change = %d, want 1", len(prov.requests))
	}

	if got := s.sessionTitle(context.Background(), "a.jsonl", "replacement prompt", 300); got != "replacement prompt" {
		t.Fatalf("title after replacing first turn = %q", got)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests after replacing first turn = %d, want 2", len(prov.requests))
	}

	freshProv := &recordingTitleProvider{}
	fresh := &Server{titleProv: freshProv, titles: newTitleCache(dir)}
	if got := fresh.sessionTitle(context.Background(), "a.jsonl", "replacement prompt", 400); got != "replacement prompt" {
		t.Fatalf("persisted title = %q", got)
	}
	if len(freshProv.requests) != 0 {
		t.Fatalf("fresh server regenerated persisted title %d time(s)", len(freshProv.requests))
	}
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
