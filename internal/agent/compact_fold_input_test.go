package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// countingProvider records every summarizer call so tests can assert that a
// fold costs exactly one request.
type countingProvider struct {
	reply string
	got   []provider.Request
}

type deadlineInspectProvider struct {
	hadDeadline bool
}

type summaryChunksProvider struct {
	chunks []provider.Chunk
}

func (p *summaryChunksProvider) Name() string { return "summary-chunks" }
func (p *summaryChunksProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(p.chunks))
	for _, chunk := range p.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *deadlineInspectProvider) Name() string { return "deadline-inspect" }
func (p *deadlineInspectProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	_, p.hadDeadline = ctx.Deadline()
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "digest"}
	close(ch)
	return ch, nil
}

func TestSummaryDoesNotAddInternalWallClockDeadline(t *testing.T) {
	prov := &deadlineInspectProvider{}
	a := New(prov, tool.NewRegistry(), &Session{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}}, Options{}, event.Discard)
	if _, err := a.foldToSummary(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, ""); err != nil {
		t.Fatal(err)
	}
	if prov.hadDeadline {
		t.Fatal("summary provider context unexpectedly has an internal deadline")
	}
}

func TestSummaryCollectorStoresOnlyVisibleText(t *testing.T) {
	prov := &summaryChunksProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: "PRIVATE REASONING"},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{}`}},
		{Type: provider.ChunkText, Text: "VISIBLE DIGEST"},
		{Type: provider.ChunkDone},
	}}
	a := New(prov, tool.NewRegistry(), NewSession("system"), Options{}, event.Discard)
	got, _, err := a.summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "VISIBLE DIGEST" {
		t.Fatalf("summary = %q, want visible text only", got)
	}
}

func TestSummaryCollectorRejectsEmptyAndLengthLimitedOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []provider.Chunk
		want   string
	}{
		{
			name: "reasoning and tool call are empty",
			chunks: []provider.Chunk{
				{Type: provider.ChunkReasoning, Text: "PRIVATE REASONING"},
				{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file"}},
			},
			want: "empty output",
		},
		{
			name: "length finish",
			chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: "partial"},
				{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length"}},
			},
			want: "output token limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &summaryChunksProvider{chunks: tc.chunks}
			a := New(prov, tool.NewRegistry(), NewSession("system"), Options{}, event.Discard)
			if _, _, err := a.summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "old"}}, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("summarize error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSummaryRequestReplaysSystemToolsAndSelectedPrefix(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	reg := tool.NewRegistry()
	reg.Add(echoTool{})
	system := provider.Message{Role: provider.RoleSystem, Content: "stable system", CreatedAt: 11}
	fold := []provider.Message{
		{Role: provider.RoleUser, Content: "old task", CreatedAt: 12},
		{Role: provider.RoleAssistant, Content: "old work", CreatedAt: 13},
	}
	a := New(prov, reg, &Session{Messages: append([]provider.Message{system}, fold...)}, Options{ContextWindow: 100_000, MaxOutputTokens: 1024}, event.Discard)

	if _, err := a.foldToSummary(context.Background(), fold, "keep exact identifiers"); err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(prov.got))
	}
	req := prov.got[0]
	if len(req.Messages) != 4 {
		t.Fatalf("summary messages = %d, want system + 2 prefix messages + instruction", len(req.Messages))
	}
	wantPrefix := []provider.Message{system, fold[0], fold[1]}
	for i := range wantPrefix {
		wantPrefix[i].CreatedAt = 0
		if !reflect.DeepEqual(req.Messages[i], wantPrefix[i]) {
			t.Fatalf("prefix message %d = %+v, want %+v", i, req.Messages[i], wantPrefix[i])
		}
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != provider.RoleUser || !strings.Contains(last.Content, "keep exact identifiers") || !strings.Contains(last.Content, "Do not call tools") {
		t.Fatalf("final compaction instruction = %+v", last)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "echo" {
		t.Fatalf("summary tools = %+v, want normal echo schema", req.Tools)
	}
	if req.MaxTokens != summaryOutputMaxTokens {
		t.Fatalf("summary max tokens = %d, want fixed cap %d", req.MaxTokens, summaryOutputMaxTokens)
	}
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
	if body := joinContents(prov.got[0].Messages); strings.Contains(body, snippedMarker) {
		t.Fatal("an under-budget fold must reach the summarizer unshortened")
	}
}

func TestManualFoldDoesNotPrivatelyShortenToolResults(t *testing.T) {
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
	body := joinContents(prov.got[0].Messages)
	if strings.Contains(body, snippedMarker) || strings.Contains(body, toolPruneMarker) {
		t.Fatalf("manual summary input was privately pruned:\n%.300q", body)
	}
	if !strings.Contains(body, "line 5 filler") {
		t.Fatalf("complete tool results did not reach summarizer:\n%.300q", body)
	}
}

func TestHugeFoldNeverMultiSpan(t *testing.T) {
	// Even a very large fold gets at most one complete-prefix provider request.
	// If it cannot fit, the transaction fails rather than shortening or splitting.
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
	if !strings.Contains(prov.got[0].Messages[len(prov.got[0].Messages)-1].Content, "focus on the parser") {
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
