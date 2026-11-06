package boot

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

type bulkReplyProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *bulkReplyProvider) Name() string { return "boot-archive-test" }

func (p *bulkReplyProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: strings.Repeat("an assistant answer worth keeping out of context ", 600)}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *bulkReplyProvider) lastRequestMessageWith(text string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		return ""
	}
	for _, m := range p.reqs[len(p.reqs)-1].Messages {
		if strings.Contains(m.Content, text) {
			return m.Content
		}
	}
	return ""
}

func (p *bulkReplyProvider) summaryRequests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, req := range p.reqs {
		if req.Summary {
			n++
		}
	}
	return n
}

// TestEffectCompactArchivesInsteadOfSummarizing pins the fold at the boundary a
// user reaches: through the real assembly, /compact asks the model for no
// digest, and the folded work is a file the model can read back.
func TestEffectCompactArchivesInsteadOfSummarizing(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &bulkReplyProvider{}
	provider.Register("boot-archive-test", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-archive-test"
model = "x"
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	for i := range 10 {
		if err := ctrl.Run(context.Background(), "continue "+strings.Repeat("x", i+1)); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}

	verdict, err := ctrl.Compact(context.Background(), agent.CompactRequest{})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !verdict.Compacted() {
		t.Fatalf("verdict = %+v, want a fold", verdict)
	}
	if n := rec.summaryRequests(); n != 0 {
		t.Fatalf("a manual compaction made %d summarizer request(s)", n)
	}

	if err := ctrl.Run(context.Background(), "what is left to do"); err != nil {
		t.Fatalf("Run after compact: %v", err)
	}
	pointer := rec.lastRequestMessageWith("archived, not summarized")
	if pointer == "" {
		t.Fatal("the request after the fold does not carry the archive pointer")
	}
	var path string
	for line := range strings.SplitSeq(pointer, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Archive: "); ok {
			path = rest
		}
	}
	if path == "" {
		t.Fatalf("the pointer names no archive file:\n%s", pointer)
	}
	if !strings.HasPrefix(path, dir) {
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "an assistant answer worth keeping out of context") {
		t.Fatalf("the archive the pointer names does not hold the folded work (%v)", err)
	}
}
