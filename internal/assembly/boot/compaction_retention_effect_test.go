package boot

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// compactionEffectProvider answers ordinary turns with enough text to drive the
// window past the compaction trigger, and summarizer turns with a digest that
// deliberately records nothing — so a constraint can only reach a later request
// by surviving verbatim.
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
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.bulk}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *compactionEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// TestEffectConstraintSurvivesCompactionThroughRealBuild is the final-boundary
// proof: a constraint stated on an early turn must still appear verbatim in a
// provider request issued after compaction folded that turn's neighbourhood.
// The summarizer here records nothing, so a digest cannot carry it — component
// correctness in the partition is not the same as the model still being told.
func TestEffectConstraintSurvivesCompactionThroughRealBuild(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-compaction-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	// 32000 stays under the 64K threshold where the recent tail takes its 32K
	// floor, which would otherwise swallow the whole transcript and never fold.
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5

[[providers]]
name = "test-model"
kind = "boot-compaction-effect"
model = "x"
context_window = 32000
`)
	approveWorkspaceAt(t, home, dir)

	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

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

	var archive string
	for _, m := range reqs[compacted].Messages {
		if strings.Contains(m.Content, constraint) {
			t.Fatalf("a fold retains nothing, yet the post-compaction request still carries the constraint: %s",
				messageDigest(reqs[compacted].Messages))
		}
		if i := strings.Index(m.Content, "Archive: "); i >= 0 {
			archive = strings.TrimSpace(strings.SplitN(m.Content[i+len("Archive: "):], "\n", 2)[0])
		}
	}
	if archive == "" {
		t.Fatalf("the post-compaction request names no archive: %s", messageDigest(reqs[compacted].Messages))
	}
	body, err := os.ReadFile(archive)
	if err != nil || !strings.Contains(string(body), constraint) {
		t.Fatalf("the archive %s does not hold the constraint verbatim (%v)", archive, err)
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
