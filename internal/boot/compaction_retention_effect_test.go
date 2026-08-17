package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// compactionEffectProvider answers ordinary turns with enough text to drive the
// window past the compaction trigger, and summarizer turns with a digest that
// deliberately records nothing. This proves old user turns are summary-owned
// rather than silently pinned verbatim by the host.
type compactionEffectProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	bulk string
}

func (p *compactionEffectProvider) Name() string { return "boot-compaction-effect" }

func (p *compactionEffectProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	text := p.bulk
	if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[len(req.Messages)-1].Content, "Compact the preceding conversation prefix") {
		text = "## Standing facts\n- none recorded"
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *compactionEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// TestOldConstraintIsSummaryOwnedAfterCompactionThroughRealBuild proves an old
// user constraint enters the summary region and is not separately preserved.
// A useful summarizer should retain it; this deliberately lossy fixture makes
// accidental host-side verbatim protection observable.
func TestOldConstraintIsSummaryOwnedAfterCompactionThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-compaction-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	// 32000 leaves enough history outside the fixed 16% retained tail to fold.
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "boot-compaction-effect"
model = "x"
context_window = 32000
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// The constraint is the second user turn so it exercises the old-history
	// fold region rather than the system prefix.
	const constraint = "standing constraint: never change the public API"
	for _, prompt := range []string{"start the task", constraint,
		"keep going", "keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}

	reqs := rec.requests()
	compacted := -1
	for i, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "<compaction-summary>") {
				compacted = i
			}
		}
	}
	if compacted < 0 {
		t.Fatalf("no request carried a digest; the fixture never compacted (%d requests)", len(reqs))
	}

	var found bool
	for _, m := range reqs[compacted].Messages {
		if strings.Contains(m.Content, constraint) {
			found = true
		}
	}
	if found {
		t.Fatalf("old user constraint was preserved verbatim outside the deliberately lossy summary.\nmessages=%s",
			messageDigest(reqs[compacted].Messages))
	}
}

func messageDigest(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		content := m.Content
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		b.WriteString("\n  " + string(m.Role) + ": " + content)
	}
	return b.String()
}
