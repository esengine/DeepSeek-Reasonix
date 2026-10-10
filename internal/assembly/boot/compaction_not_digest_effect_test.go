package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const pseudoToolCall = "<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"read_file\">\n<｜｜DSML｜｜ parameter name=\"path\">internal/a.go</｜｜DSML｜｜ parameter>\n</｜｜DSML｜｜ invoke>\n</｜｜DSML｜｜ calls>"

// notDigestProvider answers its first summary request the way a model carrying
// on the agent's work does, and later ones with a briefing.
type notDigestProvider struct {
	mu        sync.Mutex
	reqs      []provider.Request
	summaries int
	bulk      string
}

func (p *notDigestProvider) Name() string { return "boot-not-digest" }

func (p *notDigestProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	text := p.bulk
	if isSummaryRequest(req) {
		p.summaries++
		text = "## Goal\n- keep working"
		if p.summaries == 1 {
			text = pseudoToolCall
		}
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A summarizer answer that is not a briefing is a failed summary: it is typed,
// it is billed to the compaction, and the answer never reaches a later request
// as the digest. The request that asks for the briefing ends on that ask.
func TestEffectSummaryThatIsNotABriefingNeverBecomesTheDigest(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	kind := uniqueKind("boot-not-digest")
	rec := &notDigestProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 32000
`)
	approveWorkspaceAt(t, home, dir)

	var mu sync.Mutex
	var codes []string
	var billed int
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil && e.Maintenance.Status == "failed":
			codes = append(codes, e.Maintenance.Code)
		case e.Kind == event.Usage && e.UsageSource == event.UsageSourceCompaction:
			billed++
		}
	})
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	for i := range 16 {
		if err := ctrl.Run(context.Background(), "keep going"); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}

	mu.Lock()
	gotCodes, gotBilled := append([]string(nil), codes...), billed
	mu.Unlock()
	if len(gotCodes) == 0 || gotCodes[0] != "summary_not_digest" {
		t.Fatalf("failure codes %v, want the first to be summary_not_digest", gotCodes)
	}
	if gotBilled < 1 {
		t.Fatalf("the rejected summary was not billed to the compaction (%d usage events)", gotBilled)
	}
	installed := false
	for _, req := range rec.reqs {
		if isSummaryRequest(req) {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != provider.RoleUser || !strings.Contains(last.Content, "Reply now with the briefing") {
				t.Fatalf("a summary request does not end on the ask: %q", last.Content[max(0, len(last.Content)-160):])
			}
			continue
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "DSML") {
				t.Fatalf("the pseudo tool call reached a later request:\n%s", messageDigest(req.Messages))
			}
			if strings.Contains(m.Content, "<compaction-summary>") && strings.Contains(m.Content, "keep working") {
				installed = true
			}
		}
	}
	if !installed {
		t.Fatalf("the retry after the rejection never installed a briefing (%d requests, %d summaries)", len(rec.reqs), rec.summaries)
	}
}
