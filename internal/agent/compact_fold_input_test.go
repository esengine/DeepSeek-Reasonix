package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// countingProvider records every summarizer call so tests can assert how many
// calls one fold cost and what each of them was asked to read.
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
	// The ordinary case must not change: a fold that fits one call is sent
	// whole, with its tool results intact.
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

func TestOversizedFoldShortensToolResultsBeforeSplitting(t *testing.T) {
	// Escalation order matters: giving up tool-result bulk keeps the whole
	// history in one call, which is cheaper and loses less than splitting.
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 24000, prov)
	fold := foldOfToolResults(6, 300)

	res, err := a.foldToSummary(context.Background(), fold, "")
	if err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want one call after shortening", len(prov.got), res.Spans)
	}
	body := prov.got[0].Messages[1].Content
	if !strings.Contains(body, snippedMarker) {
		t.Fatalf("tool results were not shortened for the summarizer:\n%.300q", body)
	}
	if !strings.Contains(body, "line 0 filler") || !strings.Contains(body, "line 5 filler") {
		t.Fatal("shortening dropped whole tool results instead of their middles")
	}
}

func TestHugeFoldIsSummarizedInBoundedSpans(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 64000, prov)
	fold := foldOfToolResults(60, 400)

	res, err := a.foldToSummary(context.Background(), fold, "focus on the parser")
	if err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if res.Spans < 2 {
		t.Fatalf("spans = %d, want the fold split", res.Spans)
	}
	if res.Spans > maxSummarySpans {
		t.Fatalf("spans = %d, want at most %d", res.Spans, maxSummarySpans)
	}
	if len(prov.got) != res.Spans+1 {
		t.Fatalf("requests = %d, want %d span calls plus one merge", len(prov.got), res.Spans)
	}
	budget := a.summaryInputBudget("")
	for i, req := range prov.got[:res.Spans] {
		if got := estimateTextTokens(req.Messages[1].Content); got > budget {
			t.Fatalf("span %d input = %d tokens est., over the %d budget", i+1, got, budget)
		}
		if !strings.Contains(req.Messages[0].Content, "focus on the parser") {
			t.Fatalf("span %d lost the caller's focus instructions", i+1)
		}
	}
	merge := prov.got[len(prov.got)-1]
	if !strings.Contains(merge.Messages[0].Content, "consecutive parts of ONE conversation") {
		t.Fatal("the final call was not the merge pass")
	}
	if !strings.Contains(merge.Messages[1].Content, "[part 1 of") {
		t.Fatalf("merge input missing the part digests:\n%.200q", merge.Messages[1].Content)
	}
	if res.Text == "" {
		t.Fatal("span summarization returned no digest")
	}
}

func TestSpanTruncationKeepsHeadAndTailAndSaysWhatItDropped(t *testing.T) {
	span := make([]provider.Message, 0, 12)
	for i := range 12 {
		span = append(span, provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("step %02d %s", i, strings.Repeat("x", 400))})
	}
	out := truncateSpanToBudget(span, 1500)
	if estimateMessagesTokens(out) > 1500 {
		t.Fatalf("truncated span is %d tokens est., over budget", estimateMessagesTokens(out))
	}
	joined := renderTranscript(out)
	if !strings.Contains(joined, "step 00") || !strings.Contains(joined, "step 11") {
		t.Fatalf("truncation must keep the span's head and tail:\n%.300q", joined)
	}
	if !strings.Contains(joined, "messages of this part omitted") {
		t.Fatal("truncation dropped messages without saying so")
	}
}

func TestSingleOversizedMessageIsTruncatedNotDropped(t *testing.T) {
	m := provider.Message{Role: provider.RoleUser, Content: "HEAD" + strings.Repeat("z", 20000) + "TAIL"}
	spans := splitIntoSummarySpans([]provider.Message{m}, 1000)
	if len(spans) != 1 || len(spans[0]) != 1 {
		t.Fatalf("spans = %+v, want one truncated message", spans)
	}
	got := spans[0][0].Content
	if estimateTextTokens(got) > 1000 {
		t.Fatalf("truncated message is %d tokens est., over budget", estimateTextTokens(got))
	}
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") {
		t.Fatalf("truncation must keep the head and tail:\n%.200q", got)
	}
	if !strings.Contains(got, "characters omitted") {
		t.Fatal("truncation dropped content without saying so")
	}
}

func TestNoContextWindowLeavesTheFoldUnbounded(t *testing.T) {
	// Without a declared window there is no overflow to protect against, and
	// bounding would silently shorten a manual /compact.
	prov := &countingProvider{reply: "digest"}
	a := New(prov, nil, &Session{}, Options{}, event.Discard)
	fold := foldOfToolResults(40, 400)

	res, err := a.foldToSummary(context.Background(), fold, "")
	if err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 || res.Spans != 1 {
		t.Fatalf("requests=%d spans=%d, want one unbounded call", len(prov.got), res.Spans)
	}
	if strings.Contains(prov.got[0].Messages[1].Content, snippedMarker) {
		t.Fatal("an unbounded fold must not be shortened")
	}
}
