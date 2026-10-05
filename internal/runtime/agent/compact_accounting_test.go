package agent

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type scriptedReply struct {
	text  string
	usage *provider.Usage
	fail  error
}

// scriptedSummarizer answers each summarizer call from its own script, so a
// test can bill the two halves of a repair differently and tell them apart.
type scriptedSummarizer struct {
	mu      sync.Mutex
	replies []scriptedReply
	calls   int
}

func (p *scriptedSummarizer) Name() string { return "scripted" }

func (p *scriptedSummarizer) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	at := p.calls
	p.calls++
	p.mu.Unlock()
	if at >= len(p.replies) {
		return nil, fmt.Errorf("unscripted summarizer call %d", at+1)
	}
	r := p.replies[at]
	ch := make(chan provider.Chunk, 4)
	if r.text != "" {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: r.text}
	}
	if r.usage != nil {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: r.usage}
	}
	if r.fail != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: r.fail}
	}
	close(ch)
	return ch, nil
}

const (
	digestComplete   = "changed internal/parser/lexer.go and internal/parser/reader.go; go test failed"
	digestMissesOne  = "reworked internal/parser/lexer.go along the way"
	digestStillShort = "reworked internal/parser/lexer.go once more"
)

func billed(prompt, completion int) *provider.Usage {
	return &provider.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
}

// ledgerTotal is what the usage ledger received, which is the number a session's
// cost is actually built from.
func ledgerTotal(sink *recordSink) (calls, input, output int) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, e := range sink.evs {
		if e.Kind != event.Usage || e.UsageSource != event.UsageSourceCompaction || e.Usage == nil {
			continue
		}
		calls++
		input += e.Usage.PromptTokens
		output += e.Usage.CompletionTokens
	}
	return calls, input, output
}

func foldWithSpend(t *testing.T, replies []scriptedReply) (CompactionTelemetry, *recordSink, error) {
	t.Helper()
	sink := &recordSink{}
	a := New(&scriptedSummarizer{replies: replies}, coverageRegistry(), &sessionstore.Session{},
		Options{ContextWindow: 200000}, sink)
	ctx, _ := withCompactionSpend(context.Background())
	_, tele, err := a.window().foldOrDegrade(ctx, CompactionTriggerPressure, false, coverageRegion(), "", 4000)
	return tele, sink, err
}

// callsInOneTransaction drives n summarizer calls inside a single transaction.
// No production path bills a fold twice any more, and the accounting still has
// to add them up: whatever brings a second call back must land in one bill.
func callsInOneTransaction(t *testing.T, replies []scriptedReply) (sessionstore.CompactionUsage, *recordSink) {
	t.Helper()
	sink := &recordSink{}
	a := New(&scriptedSummarizer{replies: replies}, coverageRegistry(), &sessionstore.Session{},
		Options{ContextWindow: 200000}, sink)
	ctx, spend := withCompactionSpend(context.Background())
	for range replies {
		if _, err := a.window().foldToSummary(ctx, coverageRegion(), ""); err != nil && !strings.Contains(err.Error(), "stream broke") {
			t.Fatalf("foldToSummary: %v", err)
		}
	}
	return spend.read(), sink
}

// A transaction's bill is every call it made, whatever became of the answers.
// Each case bills its calls differently so a total that silently describes one
// of them cannot pass.
func TestCompactionAccountsForEverySummaryCall(t *testing.T) {
	cases := []struct {
		name       string
		replies    []scriptedReply
		wantCalls  int
		wantInput  int
		wantOutput int
	}{
		{
			name:      "a single call",
			replies:   []scriptedReply{{text: digestComplete, usage: billed(1000, 100)}},
			wantCalls: 1, wantInput: 1000, wantOutput: 100,
		},
		{
			name: "a second call whose answer is kept",
			replies: []scriptedReply{
				{text: digestMissesOne, usage: billed(1000, 100)},
				{text: digestComplete, usage: billed(1200, 120)},
			},
			wantCalls: 2, wantInput: 2200, wantOutput: 220,
		},
		{
			name: "a second call whose answer changes nothing",
			replies: []scriptedReply{
				{text: digestMissesOne, usage: billed(1000, 100)},
				{text: digestStillShort, usage: billed(1200, 120)},
			},
			wantCalls: 2, wantInput: 2200, wantOutput: 220,
		},
		{
			name: "a second call that billed and then failed",
			replies: []scriptedReply{
				{text: digestMissesOne, usage: billed(1000, 100)},
				{text: "partial", usage: billed(1200, 120), fail: errors.New("stream broke")},
			},
			wantCalls: 2, wantInput: 2200, wantOutput: 220,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, sink := callsInOneTransaction(t, c.replies)
			if got.Calls != c.wantCalls || got.InputTokens != c.wantInput || got.OutputTokens != c.wantOutput {
				t.Errorf("transaction = %+v, want calls=%d input=%d output=%d",
					got, c.wantCalls, c.wantInput, c.wantOutput)
			}
			// The gate against drift: a transaction bill and the usage ledger
			// are two readings of the same charges and may never disagree.
			calls, input, output := ledgerTotal(sink)
			if calls != got.Calls || input != got.InputTokens || output != got.OutputTokens {
				t.Errorf("ledger saw calls=%d input=%d output=%d, transaction reported %+v",
					calls, input, output, got)
			}
		})
	}
}

// A call the provider retried inside itself is one call and several requests.
// Collapsing them loses the retries; counting them as calls loses the shape.
func TestCompactionSeparatesCallsFromProviderRequests(t *testing.T) {
	retried := billed(1000, 100)
	retried.RequestCount = 3
	tele, _, err := foldWithSpend(t, []scriptedReply{{text: digestComplete, usage: retried}})
	if err != nil {
		t.Fatalf("foldOrDegrade: %v", err)
	}
	if tele.SummaryUsage.Calls != 1 || tele.SummaryUsage.RequestAttempts != 3 {
		t.Errorf("transaction = %+v, want 1 call over 3 requests", tele.SummaryUsage)
	}
	if tele.RequestCount != 3 || tele.Spans != 1 {
		t.Errorf("telemetry reqs=%d spans=%d, want 3 requests across 1 call", tele.RequestCount, tele.Spans)
	}
}

// The receipt is the durable record of a maintenance, so it is where the bill
// has to survive. A card reading it must be able to say what the compaction
// cost without going back to the session's whole usage ledger to find out.
func TestReceiptCarriesTheWholeTransactionBill(t *testing.T) {
	sess := foldableSessionOverForce(40)
	// Two changes, early enough to land in the fold: one is what the first
	// digest drops, and dropping every change would be rejected instead.
	sess.Messages = slices.Insert(sess.Messages, 2,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "w1", Name: "write_file", Arguments: `{"path":"internal/parser/lexer.go"}`},
			{ID: "w2", Name: "write_file", Arguments: `{"path":"internal/parser/reader.go"}`},
		}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "wrote"},
		provider.Message{Role: provider.RoleTool, ToolCallID: "w2", Name: "write_file", Content: "wrote"})

	sink := &recordSink{}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", writesPaths: true})
	// One scripted reply: a fold that drops a change is completed by the host,
	// so a second reply would mean a call nobody asked for.
	prov := &scriptedSummarizer{replies: []scriptedReply{
		{text: "## Files & code\n- internal/parser/lexer.go rewritten", usage: billed(9000, 400)},
	}}
	a := New(prov, reg, sess, Options{ContextWindow: 60_000, CompactRatio: 0.5, RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t)}, sink)

	if _, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil {
		t.Fatalf("compactToProjection: %v", err)
	}
	r := a.sess.win.compactionState.LastReceipt
	if r == nil || r.Status != "applied" {
		t.Fatalf("receipt = %+v, want an applied one", r)
	}
	if r.SummaryUsage.Calls != 1 {
		t.Fatalf("receipt reports %d summary calls, want one: %+v", r.SummaryUsage.Calls, r.SummaryUsage)
	}
	calls, input, output := ledgerTotal(sink)
	if r.SummaryUsage.Calls != calls || r.SummaryUsage.InputTokens != input || r.SummaryUsage.OutputTokens != output {
		t.Errorf("receipt = %+v, ledger saw calls=%d input=%d output=%d", r.SummaryUsage, calls, input, output)
	}
	// The two facts a card confuses: what was worked on, and what that cost.
	if r.InputTokens == 0 || r.SummaryUsage.InputTokens == 0 {
		t.Errorf("receipt lost one of the two sizes: context=%d billed=%d", r.InputTokens, r.SummaryUsage.InputTokens)
	}
}
