package boot

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

type promptSizeProvider struct {
	mu       sync.Mutex
	reported int
	digests  int
	mains    []provider.Request
}

func (p *promptSizeProvider) Name() string { return "boot-prompt-size" }

func (p *promptSizeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
		p.digests++
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Summary\nSUMMARY of the earlier work."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	p.mains = append(p.mains, req)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: p.reported, TotalTokens: p.reported + 1}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *promptSizeProvider) counts() (digests, mains int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.digests, len(p.mains)
}

// imageSession is about 70k estimated tokens that include one screenshot,
// which keeps the host from learning a tokens-per-character ratio from it.
func imageSession(t *testing.T) *sessionstore.Session {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	shot := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "BASE"},
		{Role: provider.RoleUser, Content: "look at the screen"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "shot", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "shot", Content: "screenshot", Images: []string{shot}},
		{Role: provider.RoleAssistant, Content: "I see it."},
	}
	for range 14 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "next question " + strings.Repeat("q", 100)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 20_000)},
		)
	}
	return &sessionstore.Session{Messages: msgs}
}

func foldsAfterTwoTurns(t *testing.T, reported int) (digests int, second provider.Request) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &promptSizeProvider{reported: reported}
	kind := "boot-reporting-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 100000
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Resume(imageSession(t), filepath.Join(dir, "sessions", "image.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := ctrl.RunTurn(context.Background(), "first"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if d, _ := prov.counts(); d != 0 {
		t.Fatalf("a fold ran before any usage was reported: digests=%d", d)
	}
	if err := ctrl.RunTurn(context.Background(), "second"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	d, mains := prov.counts()
	if mains < 2 {
		t.Fatalf("main requests = %d, want 2", mains)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	return d, prov.mains[len(prov.mains)-1]
}

func TestEffectReportedUsagePastTriggerFoldsDespiteLowLocalEstimate(t *testing.T) {
	digests, second := foldsAfterTwoTurns(t, 92_000)
	if digests != 1 {
		t.Fatalf("digest requests = %d; the provider reported 92k against an 85k trigger and the context was not folded", digests)
	}
	summarized, bulky := false, 0
	for _, m := range second.Messages {
		summarized = summarized || strings.Contains(m.Content, "SUMMARY of the earlier work.")
		if len(m.Content) >= 20_000 {
			bulky++
		}
	}
	if !summarized {
		t.Fatal("the folded view did not reach the provider request")
	}
	if bulky >= 14 {
		t.Fatalf("the request still carries all %d bulky replies", bulky)
	}
}

func TestEffectReportedUsageBelowTriggerLeavesContextAlone(t *testing.T) {
	if digests, _ := foldsAfterTwoTurns(t, 60_000); digests != 0 {
		t.Fatalf("digest requests = %d with the provider reporting 60k of 100k", digests)
	}
}
