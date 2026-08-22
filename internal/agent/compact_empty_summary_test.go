package agent

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// scriptStream is one Stream call's chunk script.
type scriptStream struct {
	chunks []provider.Chunk
}

// sequenceProvider serves one scriptStream per Stream call and counts calls.
type sequenceProvider struct {
	scripts []scriptStream
	calls   int
}

func (p *sequenceProvider) Name() string { return "sequence" }
func (p *sequenceProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	i := p.calls
	p.calls++
	if i >= len(p.scripts) {
		return nil, errors.New("sequenceProvider: no script")
	}
	ch := make(chan provider.Chunk, len(p.scripts[i].chunks))
	for _, c := range p.scripts[i].chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func newSummaryTestAgent(p provider.Provider) *Agent {
	return New(p, tool.NewRegistry(), &Session{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}}, Options{}, event.Discard)
}

// A reasoning-first model can spend the whole output budget thinking and
// finish with zero visible text. Failing the fold there wedges the session —
// the context stays over the limit and every later turn trips the same
// error — so the reasoning text is the last-resort summary source.
func TestSummarizeFallsBackToReasoningWhenVisibleTextIsEmpty(t *testing.T) {
	prov := &sequenceProvider{scripts: []scriptStream{{
		chunks: []provider.Chunk{
			{Type: provider.ChunkReasoning, Text: "The user fixed the login bug in auth.go; tests pass; remaining work is the rate limiter."},
			{Type: provider.ChunkDone},
		},
	}}}
	a := newSummaryTestAgent(prov)

	summary, _, err := a.summarizeOnce(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, "")
	if err != nil {
		t.Fatalf("reasoning-only finish must not fail the summary: %v", err)
	}
	if summary != "The user fixed the login bug in auth.go; tests pass; remaining work is the rate limiter." {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if prov.calls != 1 {
		t.Fatalf("reasoning fallback must not need a retry, calls = %d", prov.calls)
	}
}

// A transient empty finish (load-shed gateway, keep-alive hiccup) gets
// exactly one retry before the fold gives up.
func TestSummarizeOnceRetriesOneEmptyFinish(t *testing.T) {
	prov := &sequenceProvider{scripts: []scriptStream{
		{chunks: []provider.Chunk{{Type: provider.ChunkDone}}},
		{chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "digest"},
			{Type: provider.ChunkDone},
		}},
	}}
	a := newSummaryTestAgent(prov)

	summary, _, err := a.summarizeOnce(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, "")
	if err != nil {
		t.Fatalf("transient empty finish must recover on retry: %v", err)
	}
	if summary != "digest" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if prov.calls != 2 {
		t.Fatalf("expected exactly one retry (2 calls), got %d", prov.calls)
	}
}

// A persistently empty endpoint still surfaces the empty-output error — the
// retry is capped at one, so compaction latency does not double forever.
func TestSummarizeOnceCapsRetryAtOne(t *testing.T) {
	prov := &sequenceProvider{scripts: []scriptStream{
		{chunks: []provider.Chunk{{Type: provider.ChunkDone}}},
		{chunks: []provider.Chunk{{Type: provider.ChunkDone}}},
		{chunks: []provider.Chunk{{Type: provider.ChunkDone}}},
	}}
	a := newSummaryTestAgent(prov)

	_, _, err := a.summarizeOnce(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, "")
	if !errors.Is(err, errSummaryEmptyOutput) {
		t.Fatalf("persistent empty finish must report the sentinel, got %v", err)
	}
	if prov.calls != 2 {
		t.Fatalf("retry must be capped at one extra call, got %d", prov.calls)
	}
}
