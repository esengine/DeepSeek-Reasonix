package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// countingProvider records every summarizer call so tests can assert that a
// fold costs exactly one request.
type countingProvider struct {
	reply string
	got   []provider.Request
}

func (p *countingProvider) Name() string { return "counting" }

func (p *countingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.got = append(p.got, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf("%s %d", p.reply, len(p.got))}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func foldOfToolResults(n, size int) []provider.Message {
	fold := make([]provider.Message, 0, n*2)
	for i := range n {
		fold = append(fold,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: fmt.Sprint(i), Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: fmt.Sprint(i), Name: "read_file", Content: strings.Repeat(fmt.Sprintf("line %d filler\n", i), size)},
		)
	}
	return fold
}

func newFoldAgent(t *testing.T, window int, prov provider.Provider) *Agent {
	t.Helper()
	return New(prov, nil, &Session{}, Options{ContextWindow: window}, event.Discard)
}

func TestFoldUnderBudgetIsSummarizedVerbatimInOneCall(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 200000, prov)
	fold := foldOfToolResults(3, 40)

	res, err := a.foldToSummary(context.Background(), fold, "")
	if err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want a single call", len(prov.got), res.Spans)
	}
	if body := prov.got[0].Messages[1].Content; strings.Contains(body, snippedMarker) {
		t.Fatal("an under-budget fold must reach the summarizer unshortened")
	}
}

func TestOversizedFoldShortensToolResultsInOneCall(t *testing.T) {
	// Shortening is for the summarizer input only; never multi-span, never prune.
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 24000, prov)
	fold := foldOfToolResults(6, 300)

	res, err := a.foldToSummary(context.Background(), fold, "")
	if err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want exactly one call", len(prov.got), res.Spans)
	}
	body := prov.got[0].Messages[1].Content
	if !strings.Contains(body, snippedMarker) {
		t.Fatalf("tool results were not shortened for the summarizer:\n%.300q", body)
	}
}

func TestHugeFoldNeverMultiSpan(t *testing.T) {
	// Even a very large fold gets at most one provider request. If it still
	// cannot fit after shortening, the transaction fails rather than splitting.
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 32000, prov)
	fold := foldOfToolResults(80, 800)

	res, err := a.foldToSummary(context.Background(), fold, "focus on the parser")
	if err != nil {
		// Failure without a second attempt is acceptable for an unfittable fold.
		if len(prov.got) != 0 {
			t.Fatalf("failed fold still made %d provider requests", len(prov.got))
		}
		return
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want at most one call", len(prov.got), res.Spans)
	}
	if !strings.Contains(prov.got[0].Messages[0].Content, "focus on the parser") {
		t.Fatal("focus instructions lost")
	}
}

func TestNoContextWindowLeavesTheFoldUnbounded(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := New(prov, nil, &Session{}, Options{}, event.Discard)
	fold := foldOfToolResults(40, 400)

	res, err := a.foldToSummary(context.Background(), fold, "")
	if err != nil {
		// Without a window the input budget is 0 and the single-call path
		// refuses before paying for a request.
		if len(prov.got) != 0 {
			t.Fatalf("no-window failure still called provider %d times", len(prov.got))
		}
		return
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want one unbounded call", len(prov.got), res.Spans)
	}
}

func TestSummarizeOnceNoRetry(t *testing.T) {
	prov := &failOnceProvider{}
	a := newFoldAgent(t, 200000, prov)
	_, _, err := a.summarizeOnce(context.Background(), []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
	}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want exactly 1 (no application-layer retry)", prov.calls)
	}
}

type failOnceProvider struct{ calls int }

func (p *failOnceProvider) Name() string { return "fail-once" }

func (p *failOnceProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("network glitch")}
	close(ch)
	return ch, nil
}
