package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func extractTestMsg(size int, tag string) provider.Message {
	pad := max(0, size-len(tag))
	return provider.Message{Role: provider.RoleUser, Content: tag + strings.Repeat("x", pad)}
}

// Small transcripts stay in one chunk: the fast path must cover every message.
func TestSplitExtractChunksSingleChunk(t *testing.T) {
	msgs := []provider.Message{
		extractTestMsg(100, "a"),
		extractTestMsg(100, "b"),
		extractTestMsg(100, "c"),
	}
	chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, provider.SharedWindowInputPolicy{})
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(chunks))
	}
	if len(chunks[0]) != 3 {
		t.Fatalf("single chunk holds %d messages, want 3", len(chunks[0]))
	}
}

// Large transcripts split newest-tail-first along the exponential size table,
// never split a message, and share overlap messages at adjacent boundaries.
func TestSplitExtractChunksExponential(t *testing.T) {
	// 40 messages of ~48KiB each ≈ 1.9MiB total — enough for five chunks.
	msgs := make([]provider.Message, 40)
	for i := range msgs {
		msgs[i] = extractTestMsg(48<<10, fmt.Sprintf("<%06d>", i))
	}
	chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, provider.SharedWindowInputPolicy{})
	if len(chunks) < 4 {
		t.Fatalf("chunks = %d, want >= 4 for a 1.9MiB transcript", len(chunks))
	}
	// Every message appears in exactly one chunk, except the overlap region
	// which is duplicated across adjacent chunks; walking oldest → newest the
	// chunks must cover the transcript in order with no gaps.
	covered := 0
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
		// Find the chunk's first message in the transcript to check ordering.
		first := indexOfMessage(msgs, chunk[0])
		if first < 0 {
			t.Fatalf("chunk %d first message not found in transcript", i)
		}
		if first > covered+1 && i > 0 {
			t.Fatalf("gap between chunk %d and its predecessor: first=%d covered=%d", i, first, covered)
		}
		if i > 0 {
			// Overlap: the newer chunk (i-1) shares its head with this chunk's
			// tail — the same transcript message must appear in both.
			prevLast := indexOfMessage(msgs, chunks[i-1][len(chunks[i-1])-1])
			if prevLast < first {
				t.Fatalf("no overlap between chunk %d and %d", i-1, i)
			}
		}
		covered = indexOfMessage(msgs, chunk[len(chunk)-1])
	}
	if covered != len(msgs)-1 {
		t.Fatalf("chunks end at message %d, want %d (transcript tail must be in the newest chunk)", covered, len(msgs)-1)
	}
	// Newest chunk holds the last message; oldest holds the first.
	if indexOfMessage(msgs, chunks[len(chunks)-1][len(chunks[len(chunks)-1])-1]) != len(msgs)-1 {
		t.Fatalf("newest chunk does not end at the transcript tail")
	}
	if indexOfMessage(msgs, chunks[0][0]) != 0 {
		t.Fatalf("oldest chunk does not start at the transcript head")
	}
}

// Boundaries never split a message: every chunk is a contiguous slice of the
// original transcript.
func TestSplitExtractChunksContiguousSlices(t *testing.T) {
	msgs := make([]provider.Message, 24)
	for i := range msgs {
		msgs[i] = extractTestMsg(100<<10, fmt.Sprintf("<%06d>", i))
	}
	chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, provider.SharedWindowInputPolicy{})
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Fatalf("chunk %d empty", i)
		}
		start := indexOfMessage(msgs, chunk[0])
		if start < 0 || start+len(chunk) > len(msgs) {
			t.Fatalf("chunk %d has invalid source span [%d:%d]", i, start, start+len(chunk))
		}
		for j, msg := range chunk {
			want := msgs[start+j]
			if msg.Role != want.Role || msg.Content != want.Content {
				t.Fatalf("chunk %d message %d is not source message %d", i, j, start+j)
			}
		}
	}
}

func indexOfMessage(msgs []provider.Message, target provider.Message) int {
	for i := range msgs {
		if msgs[i].Content == target.Content && msgs[i].Role == target.Role {
			return i
		}
	}
	return -1
}

// extractStubProvider fails the first `failFirst` summarize requests with the
// output-truncation signal (FinishReason=length, the #9082 follow-up failure
// mode on very large sessions), then replies normally. streamErr, when set,
// is a non-retriable transport failure surfaced on every call. Every request's
// message count is recorded so merge-grouping tests can assert the merge
// request never carried the whole fragment set.
type extractStubProvider struct {
	mu        sync.Mutex
	calls     int
	failFirst int
	streamErr error
	reply     string
	msgLens   []int
	reqEsts   []int
	requests  []provider.Request
}

func (p *extractStubProvider) Name() string { return "extract-stub" }

func (p *extractStubProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	p.msgLens = append(p.msgLens, len(req.Messages))
	p.reqEsts = append(p.reqEsts, estimateMessagesTokens(req.Messages))
	requestCopy := req
	requestCopy.Messages = append([]provider.Message(nil), req.Messages...)
	p.requests = append(p.requests, requestCopy)
	n := p.calls
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if p.streamErr != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: p.streamErr}
		close(ch)
		return ch, nil
	}
	if n <= p.failFirst {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens: 10, TotalTokens: 10, CacheHitTokens: 3, CacheMissTokens: 7,
			FinishReason: "length", RequestCount: 1,
		}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.reply}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
		CacheHitTokens: 3, CacheMissTokens: 7, RequestCount: 1,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func requestContains(req provider.Request, marker string) bool {
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, marker) {
			return true
		}
	}
	return false
}

func assertActualToolResultSurvives(t *testing.T, msgs []provider.Message, callID, marker string) {
	t.Helper()
	wire := provider.SanitizeToolPairing(msgs)
	for i, msg := range wire {
		if msg.Role != provider.RoleAssistant {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.ID != callID {
				continue
			}
			if i+1 >= len(wire) || wire[i+1].Role != provider.RoleTool || wire[i+1].ToolCallID != callID || !strings.Contains(wire[i+1].Content, marker) {
				t.Fatalf("tool result %q did not survive sanitization: %+v", marker, wire)
			}
			return
		}
	}
	t.Fatalf("tool call %q missing after sanitization: %+v", callID, wire)
}

func extractStubSession() *Session {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task one"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer one"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task two"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer two"})
	return sess
}

func TestChunkedFoldSummarySplitsOnOutputTruncation(t *testing.T) {
	prov := &extractStubProvider{failFirst: 1, reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	res, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), compactionInstruction, nil)
	if err != nil {
		t.Fatalf("chunkedFoldSummary: %v", err)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatal("empty summary after split recovery")
	}
	// 1 failing root + 2 half fragments + 1 merge = 4 calls.
	if prov.calls != 4 {
		t.Fatalf("provider calls = %d, want 4 (fail, two halves, merge)", prov.calls)
	}
}

func TestSplitExtractChunksKeepsToolTurnAtomic(t *testing.T) {
	const marker = "ACTUAL-TOOL-RESULT"
	msgs := []provider.Message{
		extractTestMsg(200<<10, "old"),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: marker + strings.Repeat("r", 80<<10)},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	chunks := splitExtractChunks(msgs, extractChunkOverlapBytes, provider.SharedWindowInputPolicy{})
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want a boundary around the oversized tool turn", len(chunks))
	}
	seen := 0
	for _, chunk := range chunks {
		containsMarker := false
		for _, msg := range chunk {
			containsMarker = containsMarker || strings.Contains(msg.Content, marker)
		}
		if containsMarker {
			seen++
			assertActualToolResultSurvives(t, chunk, "c1", marker)
		}
	}
	if seen == 0 {
		t.Fatal("no chunk retained the actual tool result")
	}
}

func TestSplitExtractFragmentKeepsToolTurnAtomic(t *testing.T) {
	const marker = "ACTUAL-SPLIT-RESULT"
	chunk := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: marker},
		{Role: provider.RoleUser, Content: "next"},
	}
	left, right, ok := splitExtractFragment(chunk)
	if !ok || len(left) != 2 || len(right) != 1 {
		t.Fatalf("split = (%d, %d, %v), want (2, 1, true)", len(left), len(right), ok)
	}
	assertActualToolResultSurvives(t, left, "c1", marker)
	for _, msg := range right {
		if msg.Role == provider.RoleTool {
			t.Fatalf("right half contains an orphan tool result: %+v", right)
		}
	}
}

func TestChunkedFoldSummaryDoesNotRetryTransportErrors(t *testing.T) {
	prov := &extractStubProvider{streamErr: errors.New("provider down")}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	if _, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), compactionInstruction, nil); err == nil {
		t.Fatal("expected the transport error to surface")
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (no split retry on transport errors)", prov.calls)
	}
}

func TestChunkedFoldSummarySplitsDeepOnRepeatedTruncation(t *testing.T) {
	prov := &extractStubProvider{failFirst: 2, reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	res, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), compactionInstruction, nil)
	if err != nil {
		t.Fatalf("chunkedFoldSummary: %v", err)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatal("empty summary after deep split recovery")
	}
	if prov.calls < 6 {
		t.Fatalf("provider calls = %d, want a deep split (>=6)", prov.calls)
	}
}

func TestChunkedFoldSummarySingleMessageFragmentCannotSplit(t *testing.T) {
	prov := &extractStubProvider{failFirst: 99, reply: "digest"}
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "only"})
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	if _, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), compactionInstruction, nil); err == nil {
		t.Fatal("expected failure when every split level truncates and no split remains")
	}
}

func TestChunkedFoldSummarySingleAtomicToolTurnCannotSplit(t *testing.T) {
	prov := &extractStubProvider{failFirst: 99, reply: "digest"}
	a := New(prov, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	chunk := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "actual"},
	}
	run := newChunkedSummaryRun(a)
	if _, err := a.extractFragmentResilient(context.Background(), chunk, extractFragmentInstruction(1, 1, ""), extractMergeInstruction, func(bool) {}, run, 0); err == nil {
		t.Fatal("expected failure rather than splitting one atomic tool turn")
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 for an indivisible tool turn", prov.calls)
	}
}

func TestMergeFragmentsGroupsWhenOverBudget(t *testing.T) {
	// A 2k window shrinks mergeInputBudget to ~616 tokens. Two fragment
	// briefings overflow that planning threshold but still fit the summary
	// request itself, so the tree reducer must merge the pair in one request.
	prov := &extractStubProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{ContextWindow: 2000}, event.Discard)
	parts := []string{strings.Repeat("old ", 320), strings.Repeat("new ", 320)}
	if estimateMessagesTokens(mergeDigestMessages(parts)) <= a.mergeInputBudget() {
		t.Fatal("test setup did not exceed the merge planning budget")
	}
	merged, err := a.mergeFragments(context.Background(), parts)
	if err != nil {
		t.Fatalf("mergeFragments: %v", err)
	}
	if merged != "digest" {
		t.Fatalf("merged = %q, want digest", merged)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 grouped merge", prov.calls)
	}
}

func TestChunkedFoldSummarySkipsGroupingWithinBudget(t *testing.T) {
	// Unknown window (budget = MaxInt): the merge stays a single request.
	prov := &extractStubProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	if _, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), compactionInstruction, nil); err != nil {
		t.Fatalf("chunkedFoldSummary: %v", err)
	}
	// Single chunk fast path: 1 fragment request, no merge needed.
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls)
	}
}

func TestMergeFragmentsRetriesFinalUnknownWindowMerge(t *testing.T) {
	// An unknown window skips proactive grouping. If the final whole-set
	// request still truncates, mergeGroup must split and tree-reduce instead
	// of discarding briefings that are already in hand.
	prov := &extractStubProvider{failFirst: 1, reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	merged, err := a.mergeFragments(context.Background(), []string{"one", "two", "three", "four"})
	if err != nil {
		t.Fatalf("mergeFragments: %v", err)
	}
	if merged != "digest" {
		t.Fatalf("merged = %q, want digest", merged)
	}
	// 1 failing whole-set request + 2 successful halves + 1 final merge.
	if prov.calls != 4 {
		t.Fatalf("provider calls = %d, want 4", prov.calls)
	}
}

type noProgressMergeProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *noProgressMergeProvider) Name() string { return "no-progress-merge" }

func (p *noProgressMergeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	parts := make([]string, 0, 2)
	for _, msg := range req.Messages {
		if !strings.HasPrefix(msg.Content, "<fragment index=") {
			continue
		}
		if start := strings.IndexByte(msg.Content, '\n'); start >= 0 {
			if end := strings.LastIndex(msg.Content, "\n</fragment>"); end > start {
				parts = append(parts, msg.Content[start+1:end])
			}
		}
	}
	ch := make(chan provider.Chunk, 3)
	if len(parts) >= 2 {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, TotalTokens: 10, FinishReason: "length", RequestCount: 1}}
	} else if len(parts) == 1 {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: parts[0]}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11, RequestCount: 1}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestMergeFragmentsStopsWhenRecoveryMakesNoProgress(t *testing.T) {
	prov := &noProgressMergeProvider{}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	_, err := a.mergeFragments(context.Background(), []string{"one", "two"})
	if err == nil || !strings.Contains(err.Error(), "made no progress") {
		t.Fatalf("merge error = %v, want explicit no-progress failure", err)
	}
	if prov.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 bounded calls (pair + two singletons)", prov.calls)
	}
}

func TestChunkedSummaryRunEnforcesCallBudget(t *testing.T) {
	prov := &extractStubProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	run := newChunkedSummaryRun(a)
	for i := range maxChunkedSummaryCalls {
		if _, err := run.summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "x"}}, extractMergeInstruction, 0); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if _, err := run.summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "x"}}, extractMergeInstruction, 0); err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
		t.Fatalf("budget error = %v", err)
	}
	if prov.calls != maxChunkedSummaryCalls {
		t.Fatalf("provider calls = %d, want cap %d", prov.calls, maxChunkedSummaryCalls)
	}
}

func TestChunkedFallbackPreservesFocusAndAggregatesTelemetry(t *testing.T) {
	const focus = "KEEP-FOCUS-MARKER-9082"
	prov := &extractStubProvider{failFirst: 2, reply: "digest"}
	a := New(prov, tool.NewRegistry(), extractStubSession(), Options{}, event.Discard)
	fold := a.Session().Snapshot()
	res, tele, err := a.foldSummaryWithChunkedFallback(context.Background(), CompactionTriggerManual, fold, focus, 321, SummaryInputCachePrefix)
	if err != nil {
		t.Fatalf("foldSummaryWithChunkedFallback: %v", err)
	}
	if res.InputMode != SummaryInputChunked || tele.SummaryInputMode != SummaryInputChunked {
		t.Fatalf("input modes = (%q, %q), want %q", res.InputMode, tele.SummaryInputMode, SummaryInputChunked)
	}
	if tele.FoldTokens <= 0 {
		t.Fatalf("fold tokens = %d, want original fold estimate", tele.FoldTokens)
	}
	if tele.Spans != prov.calls || tele.RequestCount != prov.calls {
		t.Fatalf("spans/requests/calls = %d/%d/%d, want exact aggregate", tele.Spans, tele.RequestCount, prov.calls)
	}
	if tele.InputTokens != prov.calls*10 || tele.CacheHitTokens != prov.calls*3 || tele.CacheMissTokens != prov.calls*7 {
		t.Fatalf("aggregated usage = %+v, calls = %d", tele, prov.calls)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("requests = %d, want initial call plus fallback", len(prov.requests))
	}
	for i, req := range prov.requests[1:] {
		if !requestContains(req, focus) {
			t.Fatalf("fallback request %d discarded focus marker", i+1)
		}
	}
}

func TestChunkedFoldSummaryIgnoresLocalRawContentForChunking(t *testing.T) {
	prov := &extractStubProvider{reply: "digest"}
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "visible-one", RawContent: strings.Repeat("r", 1<<20)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "visible-two", RawContent: strings.Repeat("p", 1<<20)})
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	if _, err := a.chunkedFoldSummary(context.Background(), a.Session().Snapshot(), "", nil); err != nil {
		t.Fatalf("chunkedFoldSummary: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want one provider-visible fragment", prov.calls)
	}
	for _, req := range prov.requests {
		for _, msg := range req.Messages {
			if msg.RawContent != "" || msg.ProviderContent != "" {
				t.Fatalf("local-only content reached fallback request: %+v", msg)
			}
		}
	}
}

func TestMergeDigestMessagesUsesDecimalFragmentIndexes(t *testing.T) {
	msgs := mergeDigestMessages([]string{"oldest", "newest"})
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	for i, msg := range msgs[1:] {
		want := fmt.Sprintf("<fragment index=%d>", i+1)
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("fragment %d content = %q, want %q", i+1, msg.Content, want)
		}
	}
}

func TestCompactFallsBackToChunkedSummaryOnTruncation(t *testing.T) {
	// The single-request summary hits the output limit; compaction must fall
	// back to the chunked extract strategy and install the projection in the
	// same session (one /compact covers over-length sessions, #9082 follow-up).
	prov := &extractStubProvider{failFirst: 1, reply: "digest"}
	sess := NewSession("sys")
	for range 12 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 6000)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 6000)})
	}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 100_000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if len(a.sess.compactionState.Projection.Messages) == 0 {
		t.Fatal("projection not installed after the chunked fallback")
	}
	if prov.calls < 4 {
		t.Fatalf("provider calls = %d, want the failed single request plus chunks and merge (>=4)", prov.calls)
	}
}
