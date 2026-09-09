package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const deepSeekOverflowBody = `{"error":{"message":"This model's maximum context length is %d tokens. However, you requested %d tokens (%d in the messages, %d in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`

// denseTokenizerProvider counts three characters per token where the agent's
// cold estimate assumes four, so a fold the estimator believes fits overflows
// on the wire exactly as #9818 reported. Its overflow reply is the parsed
// DeepSeek 400 body, so the feedback path sees what production sees.
// alwaysOverflow reports every prompt as at least the window, so each reply
// still justifies its rejection while no summary form can ever land.
type denseTokenizerProvider struct {
	mu             sync.Mutex
	window         int
	alwaysOverflow bool
	// unnumberedReplay rejects replay-form summaries with a bare overflow that
	// carries no token numbers, the shape GLM reports (#9878).
	unnumberedReplay bool
	requests         []provider.Request
	overflows        int
}

func (p *denseTokenizerProvider) Name() string { return "dense-tokenizer" }

func (p *denseTokenizerProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{
		WindowMode: provider.ContextWindowShared, AutoOutputTokens: 8192, MaxOutputTokens: 8192,
		LimitMode: provider.OutputLimitOmitWhenSafe,
	}
}

func denseTokens(req provider.Request) int {
	chars, _, _ := requestCalibrationTextShape(req, provider.SharedWindowInputPolicy{})
	return int(chars) / 3
}

func isSummaryRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[len(req.Messages)-1].Content, "Compact the preceding conversation prefix")
}

func (p *denseTokenizerProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	req.Messages = append([]provider.Message(nil), req.Messages...)
	p.requests = append(p.requests, req)
	if p.unnumberedReplay && isSummaryRequest(req) && req.Messages[0].Content != slimSummarySystemPrompt {
		p.overflows++
		return nil, &provider.ContextLimitError{APIError: &provider.APIError{
			Provider: p.Name(), Status: 400, Body: `{"error":{"code":"1261","message":"Prompt exceeds max length"}}`,
		}}
	}
	prompt := denseTokens(req)
	if p.unnumberedReplay {
		prompt = prompt * 3 / 4 // an ordinary tokenizer: only the replay form was rejected
	}
	if p.alwaysOverflow {
		prompt = max(prompt, p.window)
	}
	completion := req.MaxTokens
	if completion <= 0 {
		completion = 8192
	}
	if prompt+completion > p.window {
		p.overflows++
		body := fmt.Sprintf(deepSeekOverflowBody, p.window, prompt+completion, prompt, completion)
		limit := provider.ParseContextLimitError(&provider.APIError{Provider: p.Name(), Status: 400, Body: body})
		if limit == nil {
			return nil, fmt.Errorf("test body did not parse as a context limit: %s", body)
		}
		return nil, limit
	}
	text := "ok"
	if isSummaryRequest(req) {
		text = "- goal: keep going\n- pending: continue"
	}
	return chunks(
		provider.Chunk{Type: provider.ChunkText, Text: text},
		provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: prompt, CompletionTokens: 8, TotalTokens: prompt + 8, RequestCount: 1}},
		provider.Chunk{Type: provider.ChunkDone},
	), nil
}

func (p *denseTokenizerProvider) summaryRequests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []provider.Request
	for _, req := range p.requests {
		if isSummaryRequest(req) {
			out = append(out, req)
		}
	}
	return out
}

func requestFingerprints(reqs []provider.Request) map[string]int {
	seen := map[string]int{}
	for _, req := range reqs {
		seen[providerVisibleFingerprint(req.Messages)]++
	}
	return seen
}

func longASCIISession(turns int) *Session {
	big := strings.Repeat("alpha beta gamma delta ", 200)
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "standing constraint: keep the public API stable"})
	for i := range turns {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("step %d: %s", i, big)})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	return sess
}

// The estimator plans the largest prefix it believes fits; the provider counts
// denser and rejects it. The overflow must recalibrate the estimator and the
// re-planned request must be strictly smaller, landing a real digest without
// the fragment path and without ever repeating the rejected request.
func TestSummaryOverflowRecalibratesAndReplansSmaller(t *testing.T) {
	prov := &denseTokenizerProvider{window: 20_000}
	sess := longASCIISession(16)
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 20_000, CompactRatio: 0.8}, event.Discard)
	if est, fold, hard := a.ContextUsedTokens(), a.compactTrigger(), a.hardInputCeiling(); est < fold || est >= hard {
		t.Fatalf("fixture estimates %d tokens; want between the trigger %d and the ceiling %d", est, fold, hard)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	summaries := prov.summaryRequests()
	if prov.overflows != 1 || len(summaries) != 2 {
		t.Fatalf("overflows=%d summaries=%d, want one rejected replay and one re-planned success", prov.overflows, len(summaries))
	}
	if first, second := denseTokens(summaries[0]), denseTokens(summaries[1]); second >= first {
		t.Fatalf("re-planned summary request %d tokens is not smaller than the rejected %d", second, first)
	}
	for fp, n := range requestFingerprints(summaries) {
		if n > 1 {
			t.Fatalf("summary request %s was sent %d times", fp, n)
		}
	}
	if ratio := a.tokPerChar(); ratio < 0.3 {
		t.Fatalf("calibration ratio %.3f did not learn the provider's denser tokenizer", ratio)
	}
	r := a.sess.compactionState.LastReceipt
	if r == nil || r.Status != "applied" || r.Action != "summary" || latestDigest(a.sess.compactionState.Projection.Messages) == "" {
		t.Fatalf("receipt = %+v, want an applied summary with a digest", r)
	}
}

// An overflow reply without token numbers cannot recalibrate anything, so a
// re-plan would resend the same bytes. The ladder must skip straight to the
// transcript form and must not learn a ratio or window from zero fields.
func TestUnnumberedSummaryOverflowSkipsReplanToTranscript(t *testing.T) {
	prov := &denseTokenizerProvider{window: 20_000, unnumberedReplay: true}
	reg := tool.NewRegistry()
	reg.Add(schemaTool{})
	sess := longASCIISession(16)
	a := New(prov, reg, sess, Options{ContextWindow: 20_000, CompactRatio: 0.8}, event.Discard)

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	summaries := prov.summaryRequests()
	if prov.overflows != 1 || len(summaries) != 2 {
		t.Fatalf("overflows=%d summaries=%d, want one rejected replay and one transcript-form success", prov.overflows, len(summaries))
	}
	if slim := summaries[1]; len(slim.Tools) != 0 || len(slim.Messages) != 2 {
		t.Fatalf("second request = %d tools, %d messages; want the transcript form, not a re-planned replay", len(slim.Tools), len(slim.Messages))
	}
	if ratio := a.tokPerChar(); ratio != fallbackTokPerChar {
		t.Fatalf("calibration ratio %.3f changed on an overflow without token numbers", ratio)
	}
	if window := a.effectiveContextWindow(); window != 20_000 {
		t.Fatalf("effective window %d changed on an overflow without token numbers", window)
	}
	r := a.sess.compactionState.LastReceipt
	if r == nil || r.Status != "applied" || r.Action != "summary" || latestDigest(a.sess.compactionState.Projection.Messages) == "" {
		t.Fatalf("receipt = %+v, want an applied summary with a digest", r)
	}
}

// A provider that rejects every summary form must not trap /compact in a loop
// of identical requests: replay re-plans, then the transcript form, then the
// fragment path, and at the ceiling the truncation rescue finally lands.
func TestManualCompactOverCeilingRescuesWithoutRepeatingRequests(t *testing.T) {
	prov := &denseTokenizerProvider{window: 20_000, alwaysOverflow: true}
	sess := longASCIISession(30)
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 20_000, CompactRatio: 0.8}, event.Discard)
	if est, hard := a.ContextUsedTokens(), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture estimates %d tokens against a %d ceiling; it is not over it", est, hard)
	}

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow = %v, want the truncation rescue", err)
	}
	if !truncatedRescue(a) {
		t.Fatalf("receipt = %+v, want an applied truncation without a digest", a.sess.compactionState.LastReceipt)
	}
	if after, hard := a.ContextUsedTokens(), a.hardInputCeiling(); after >= hard {
		t.Fatalf("rescued view estimates %d tokens against a %d ceiling", after, hard)
	}
	summaries := prov.summaryRequests()
	if len(summaries) < 3 {
		t.Fatalf("summary requests = %d, want replay re-plans and the transcript form before the rescue", len(summaries))
	}
	for fp, n := range requestFingerprints(summaries) {
		if n > 1 {
			t.Fatalf("summary request %s was sent %d times", fp, n)
		}
	}
	slim := 0
	for _, req := range summaries {
		if len(req.Tools) == 0 && len(req.Messages) == 2 {
			slim++
		}
	}
	if slim != 1 {
		t.Fatalf("transcript-form summary requests = %d, want exactly one rung", slim)
	}
}

type schemaTool struct{}

func (schemaTool) Name() string        { return "read_file" }
func (schemaTool) Description() string { return "Read a file." }
func (schemaTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (schemaTool) ReadOnly() bool { return true }
func (schemaTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func TestSlimSummaryRequestIsBoundedAndToolFree(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(schemaTool{})
	a := New(&denseTokenizerProvider{window: 1 << 20}, reg, NewSession("sys"), Options{ContextWindow: 1 << 20}, event.Discard)
	body := strings.Repeat("0123456789", 3000)
	fold := []provider.Message{
		{Role: provider.RoleUser, Content: "read it"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"x"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: body, Images: []string{"data:image/png;base64,AAAA"}},
	}

	replay := a.summaryRequest(fold, "")
	if len(replay.Tools) == 0 {
		t.Fatal("replay form must carry the tool schemas the sampling request uses")
	}
	slim := a.slimSummaryRequest(fold, "")
	if len(slim.Tools) != 0 || len(slim.Messages) != 2 {
		t.Fatalf("slim form = %d tools, %d messages; want no schemas and one transcript turn", len(slim.Tools), len(slim.Messages))
	}
	text := slim.Messages[1].Content
	if !strings.Contains(text, "tool result truncated for summarization") || strings.Contains(text, "base64") {
		t.Fatal("slim transcript must cut the tool body and drop images")
	}
	if len(text) > slimToolResultRunes+2000 {
		t.Fatalf("slim transcript is %d bytes; the tool body should be bounded by %d runes", len(text), slimToolResultRunes)
	}
	if got, want := a.estimatedRequestTokens(slim), a.estimatedRequestTokens(replay); got >= want {
		t.Fatalf("slim request estimates %d tokens, not smaller than the replay's %d", got, want)
	}
}

func TestChunkedFallbackAppliesOnlyAfterTranscriptForm(t *testing.T) {
	overflow := &provider.ContextLimitError{WindowTokens: 10, PromptTokens: 20}
	if chunkedFallbackApplies(overflow, SummaryInputCachePrefix) {
		t.Fatal("a replay overflow should be re-planned, not fragmented")
	}
	if !chunkedFallbackApplies(overflow, SummaryInputSlim) {
		t.Fatal("an overflow of the transcript form has no cheaper rung left")
	}
	if !chunkedFallbackApplies(errSummaryOutputTruncated, SummaryInputCachePrefix) || !chunkedFallbackApplies(ErrCompactionRequired, SummaryInputCachePrefix) {
		t.Fatal("output truncation and local admission keep their direct fragment path")
	}
}

func TestActiveTurnFoldBoundaryKeepsNewestRounds(t *testing.T) {
	round := func(i int) []provider.Message {
		id := fmt.Sprintf("c%d", i)
		return []provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: "body"},
		}
	}
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "task", CreatedAt: 7}}
	for i := range 4 {
		msgs = append(msgs, round(i)...)
	}
	// Rounds occupy [2,4) [4,6) [6,8) [8,10); the newest two stay verbatim.
	if got := activeTurnFoldBoundary(msgs, 1, len(msgs)); got != 6 {
		t.Fatalf("boundary = %d, want 6 (fold prompt + two oldest rounds)", got)
	}
	short := msgs[:6]
	if got := activeTurnFoldBoundary(short, 1, len(short)); got != 1 {
		t.Fatalf("boundary = %d, want the turn kept whole when it has only the rounds to keep", got)
	}
	if got := activeTurnFoldBoundary(msgs, 1, 7); got != 4 {
		t.Fatalf("boundary = %d, want 4 when the fold end cuts the newest rounds off", got)
	}
}

func toolLoopSession(rounds int) *Session {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "read everything"})
	body := strings.Repeat("tool output line\n", 120)
	for i := range rounds {
		id := fmt.Sprintf("c%d", i)
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: body})
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "now summarize"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "tail"})
	return sess
}

func TestTruncateViewElidesOldestToolResultsBeforeDropping(t *testing.T) {
	sess := toolLoopSession(10)
	a := New(&denseTokenizerProvider{window: 1 << 20}, tool.NewRegistry(), sess, Options{ContextWindow: 10_000, CompactRatio: 0.5}, event.Discard)
	visible := sess.Snapshot()
	total := a.estimatedVisibleRequestTokens(visible)
	if total < 3000 {
		t.Fatalf("fixture estimates only %d tokens", total)
	}

	projected, affected := a.truncateView(visible, total*2/3)
	if affected == 0 || len(projected) != len(visible) {
		t.Fatalf("elision changed %d messages and %d->%d length; want in-place elision only", affected, len(visible), len(projected))
	}
	if !strings.HasPrefix(projected[3].Content, elidedToolResultPrefix) {
		t.Fatalf("oldest tool result was not elided: %q", projected[3].Content[:40])
	}
	newest := len(visible) - 3
	if strings.HasPrefix(projected[newest].Content, elidedToolResultPrefix) {
		t.Fatal("the protected tail's tool result must stay verbatim")
	}
	if after := a.estimatedVisibleRequestTokens(projected); after >= total*2/3 {
		t.Fatalf("elision left %d tokens, want under the %d target", after, total*2/3)
	}

}

// Text-only history gives elision nothing to cut, so the drop stage must
// remove the oldest replay units behind a marker while an earlier digest and
// the protected tail survive.
func TestTruncateViewDropsOldestUnitsWhenElisionCannotReach(t *testing.T) {
	sess := longASCIISession(6)
	a := New(&denseTokenizerProvider{window: 1 << 20}, tool.NewRegistry(), sess, Options{ContextWindow: 10_000, CompactRatio: 0.5}, event.Discard)
	visible := sess.Snapshot()
	digest := formatSummaryMessage("- earlier digest")
	withDigest := append([]provider.Message{visible[0], visible[1], digest}, visible[2:]...)
	total := a.estimatedVisibleRequestTokens(withDigest)

	dropped, affected := a.truncateView(withDigest, total/3)
	if affected == 0 || len(dropped) >= len(withDigest) {
		t.Fatalf("drop stage changed %d messages and %d->%d length; want oldest units removed", affected, len(withDigest), len(dropped))
	}
	if !strings.Contains(dropped[1].Content, "truncated to fit the context window") {
		t.Fatalf("drop stage left no marker: %q", dropped[1].Content)
	}
	if latestDigest(dropped) == "" {
		t.Fatal("an earlier compaction digest must survive truncation")
	}
	if last := dropped[len(dropped)-1]; last.Content != visible[len(visible)-1].Content {
		t.Fatal("the protected tail must stay verbatim")
	}
	if after := a.estimatedVisibleRequestTokens(dropped); after >= total/3 {
		t.Fatalf("drop stage left %d tokens, want under the %d target", after, total/3)
	}
}

func TestFailedReceiptLiftsWhenViewOutgrowsFailure(t *testing.T) {
	const window = 10_000
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 500)},
		{Role: provider.RoleUser, Content: "current"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}}
	prov := &failingSummaryProvider{}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2}, event.Discard)
	policy := ContextPreparePolicy{Trigger: CompactionTriggerPressure, ObservedInputTokens: 8600}
	a.activeTurnCreatedAt.Store(11)

	if _, err := a.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	sess.Add(provider.Message{Role: provider.RoleTool, Content: strings.Repeat("small output ", 40)})
	if _, err := a.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Fatalf("a small same-turn change made %d summary calls, want the backoff to hold", prov.calls)
	}
	sess.Add(provider.Message{Role: provider.RoleTool, Content: strings.Repeat("large output ", 400)})
	if _, err := a.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 2 {
		t.Fatalf("a view grown by over 5%% of the window made %d summary calls, want the backoff lifted", prov.calls)
	}
}
