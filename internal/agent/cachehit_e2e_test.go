package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

// echoTool is a trivial read-only tool used to drive a multi-step tool loop:
// each call appends an assistant(tool_call) + tool(result) pair to the history,
// growing the request prefix the way a real multi-turn session does.
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo back the given text" }
func (echoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}
func (echoTool) ReadOnly() bool { return true }
func (echoTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	return "echoed: " + a.Text, nil
}

// collectSink captures the per-turn Usage events plus any compaction notices the
// agent emits, so the test can replay exactly what the status line would show.
type collectSink struct {
	usages  []*provider.Usage
	notices []string
}

func (s *collectSink) Emit(e event.Event) {
	switch e.Kind {
	case event.Usage:
		if e.Usage != nil {
			s.usages = append(s.usages, e.Usage)
		}
	case event.Notice:
		s.notices = append(s.notices, e.Text)
	}
}

// --- a mock DeepSeek endpoint that derives cache-hit tokens from the byte-
// identical message prefix it shares with the previous *conversation* request.
// The reported hit rate is therefore a direct measurement of how stable the
// client keeps its request prefix turn over turn. ---

type mockDeepSeek struct {
	t            *testing.T
	prevMessages []json.RawMessage // last conversation request's messages
	reqChars     []int             // total prompt chars per conversation request
	hitChars     []int             // cached prefix chars per conversation request
	withTools    bool              // advertise the echo tool (and emit tool calls)
	reasoning    string            // chain-of-thought echoed every turn (round-tripped)
	toolRounds   int               // remaining tool-call rounds before a final answer
}

func (m *mockDeepSeek) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	// Compaction issues a tool-less summarize request whose system prompt is the
	// summarizer prompt — answer it with a short summary and DON'T let it pollute
	// the conversation-prefix bookkeeping.
	if isSummarizeRequest(body) {
		writeSSE(w, m.t,
			streamChunk(deltaText("- goal: keep going\n- decisions: none\n- pending: continue")),
			finishChunk("stop"),
			usageChunk(100, 40, 0, 100),
		)
		return
	}

	msgs := decodeMessages(body)
	common := commonPrefixMsgs(m.prevMessages, msgs)
	hitChars := charsOf(msgs[:common])
	totalChars := charsOf(msgs)
	m.prevMessages = msgs
	m.reqChars = append(m.reqChars, totalChars)
	m.hitChars = append(m.hitChars, hitChars)

	promptTok := totalChars / 4
	hitTok := hitChars / 4
	missTok := promptTok - hitTok

	emitTool := m.withTools && m.toolRounds > 0
	if emitTool {
		m.toolRounds--
	}

	chunks := []sseResp{streamChunk(deltaReasoning(m.reasoning))}
	if emitTool {
		idx := len(m.reqChars)
		chunks = append(chunks,
			streamChunk(deltaToolCall(idx, "echo", fmt.Sprintf(`{"text":"round-%d"}`, idx))),
			finishChunk("tool_calls"))
	} else {
		chunks = append(chunks,
			streamChunk(deltaText("Done.")),
			finishChunk("stop"))
	}
	chunks = append(chunks, usageChunk(promptTok, 50, hitTok, missTok))
	writeSSE(w, m.t, chunks...)
}

func (m *mockDeepSeek) tools() *tool.Registry {
	reg := tool.NewRegistry()
	if m.withTools {
		reg.Add(echoTool{})
	}
	return reg
}

// hitRate is the status-line formula: hit / (hit+miss), falling back to prompt.
func hitRate(u *provider.Usage) int {
	denom := u.CacheHitTokens + u.CacheMissTokens
	if denom == 0 {
		denom = u.PromptTokens
	}
	if denom == 0 {
		return 0
	}
	return u.CacheHitTokens * 100 / denom
}

const systemPrompt = "You are reasonix, a coding agent. Be concise and follow project conventions. " +
	"This system prompt is the cacheable head of every request and must never change between turns."

// longReasoning stands in for a deepseek-reasoner chain-of-thought that the agent
// round-trips onto the assistant turn (agent.go round-trips ReasoningContent).
const longReasoning = "Let me reason about this carefully. I will weigh the constraints, " +
	"enumerate the candidate approaches, reject the ones that violate a requirement, and then " +
	"commit to the most defensible option, double-checking it against the original goal before answering."

// TestCacheHitPrefixStable proves the standard path keeps a byte-stable prefix:
// every request re-sends the full prior history untouched, and the displayed
// hit% equals hit/prompt%. This rules out "something is breaking the cache" and
// "the display math is wrong" for the no-compaction path.
func TestCacheHitPrefixStable(t *testing.T) {
	mock := &mockDeepSeek{t: t, withTools: true, reasoning: longReasoning, toolRounds: 2}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 0 /*no compaction*/, 0)
	if err := a.Run(context.Background(), "echo a couple things then finish"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Reconstruct the requests to check prefix stability. Replay equality is
	// already encoded in hitChars==full-previous-prefix, but assert it directly.
	for i := 1; i < len(mock.reqChars); i++ {
		// On request i the cached prefix should be the ENTIRE request i-1.
		if mock.hitChars[i] != mock.reqChars[i-1] {
			t.Errorf("PREFIX BROKEN at req %d: cached %d chars but the full prior request was %d chars",
				i, mock.hitChars[i], mock.reqChars[i-1])
		}
	}
	t.Logf("prefix STABLE across %d requests — nothing in the client breaks the cache", len(mock.reqChars))

	t.Logf("==== reported usage (what the status line renders) ====")
	for i, u := range sink.usages {
		want := -1
		if u.PromptTokens > 0 {
			want = 100 * u.CacheHitTokens / u.PromptTokens
		}
		t.Logf("turn %d: prompt=%d hit=%d miss=%d → 'cache %d%%' (hit/prompt=%d%%) | %s",
			i, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens, hitRate(u), want,
			strings.TrimSpace(FormatUsageLine(u, nil, nil)))
		if u.CacheHitTokens+u.CacheMissTokens != u.PromptTokens {
			t.Errorf("display denominator mismatch: hit+miss=%d != prompt=%d (status%% would read wrong)",
				u.CacheHitTokens+u.CacheMissTokens, u.PromptTokens)
		}
	}
}

// TestCacheHitClimbsWithoutCompaction runs a long multi-turn conversation with
// compaction DISABLED and prints the hit-rate curve. With a stable prefix the
// rate should climb past 90% as history dwarfs each turn's fresh tail.
func TestCacheHitClimbsWithoutCompaction(t *testing.T) {
	mock := &mockDeepSeek{t: t, reasoning: longReasoning}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 0 /*no compaction*/, 0)

	const turns = 14
	for i := 0; i < turns; i++ {
		userMsg := "Turn " + fmt.Sprint(i) + ": " + strings.Repeat("please consider this requirement. ", 6)
		if err := a.Run(context.Background(), userMsg); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}

	t.Logf("==== hit-rate curve, NO compaction (%d turns) ====", turns)
	peak := 0
	for i, u := range sink.usages {
		r := hitRate(u)
		if r > peak {
			peak = r
		}
		t.Logf("turn %2d: prompt=%5d hit=%5d miss=%4d → cache %d%%", i, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens, r)
	}
	t.Logf("peak hit rate without compaction: %d%%", peak)
	if peak < 90 {
		t.Logf("NOTE: even with a perfectly stable prefix the rate plateaus below 90%% — "+
			"each turn's fresh tail (incl. %d-char round-tripped reasoning) is too large a share", len(longReasoning))
	}
}

// TestCacheHitSurvivesTooSmallWindow drives a long tool-loop against a window so
// small a single turn can't be summarized under it — the misconfigured regime
// that used to make compaction rewrite the prefix every step, cratering the
// cache turn after turn. The stuck guard now detects that compaction can't make
// progress, pauses it (with a notice), and lets the prefix grow append-only — so
// the hit rate recovers and stays high instead of collapsing repeatedly.
func TestCacheHitSurvivesTooSmallWindow(t *testing.T) {
	mock := &mockDeepSeek{t: t, withTools: true, reasoning: longReasoning, toolRounds: 30}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 900 /*window tok*/, 4 /*recentKeep*/)

	if err := a.Run(context.Background(), strings.Repeat("please consider this requirement. ", 6)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Logf("==== hit-rate curve, too-small window (900 tok) ====")
	collapses := 0
	for i, u := range sink.usages {
		r := hitRate(u)
		marker := ""
		if i > 0 && r+20 < hitRate(sink.usages[i-1]) {
			marker = "   <<< collapse"
			collapses++
		}
		t.Logf("step %2d: prompt=%5d hit=%5d miss=%4d → cache %3d%%%s", i, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens, r, marker)
	}

	paused := false
	for _, n := range sink.notices {
		t.Logf("notice: %s", n)
		if strings.Contains(n, "Automatic context cleanup paused") {
			paused = true
		}
	}

	// The guard caps the damage: a couple of compactions at most, not one per step.
	if collapses > 2 {
		t.Errorf("compaction cratered the cache %d times; the stuck guard should cap it at ≤2", collapses)
	}
	if !paused {
		t.Errorf("expected an auto-compaction-paused notice for the too-small window")
	}
	// Once paused, the prefix grows append-only again, so the tail of the run
	// recovers to a high, stable hit rate instead of collapsing every step.
	if n := len(sink.usages); n >= 6 {
		if tail := tailAverage(usageRates(sink.usages), 5); tail < 85 {
			t.Errorf("tail hit rate after the guard kicked in = %d%%, want ≥85%%", tail)
		}
	}
}

// TestReasoningRoundTripCost contrasts the hit-rate curve WITH vs WITHOUT the
// reasoning_content round-trip (agent.go re-sends the assistant chain-of-thought
// every turn). It quantifies how much that round-tripped CoT — assuming DeepSeek
// counts it as uncached prompt — drags the hit rate down at each turn.
func TestReasoningRoundTripCost(t *testing.T) {
	curve := func(reasoning string) []int {
		mock := &mockDeepSeek{t: t, reasoning: reasoning}
		srv := httptest.NewServer(http.HandlerFunc(mock.handler))
		defer srv.Close()
		a, sink := newAgent(t, srv.URL, mock.tools(), 0, 0)
		const turns = 12
		for i := 0; i < turns; i++ {
			if err := a.Run(context.Background(), strings.Repeat("please consider this requirement. ", 6)); err != nil {
				t.Fatalf("Run %d: %v", i, err)
			}
		}
		out := make([]int, len(sink.usages))
		for i, u := range sink.usages {
			out[i] = hitRate(u)
		}
		return out
	}

	withCoT := curve(longReasoning)
	without := curve("")

	t.Logf("==== reasoning round-trip: hit-rate cost per turn ====")
	t.Logf("turn | with reasoning round-trip | without (stripped) | delta")
	firstCross := func(c []int) int {
		for i, r := range c {
			if r >= 90 {
				return i
			}
		}
		return -1
	}
	for i := range withCoT {
		t.Logf("  %2d |          %3d%%             |       %3d%%          | +%d pts",
			i, withCoT[i], without[i], without[i]-withCoT[i])
	}
	t.Logf("turns needed to reach 90%%: with round-trip = %d, stripped = %d", firstCross(withCoT), firstCross(without))
}

// TestSessionAggregateCacheRate verifies the session-aggregate hit-rate the
// status line now shows: Agent.SessionCache() accumulates every turn's hit/miss
// (so it equals the sum of the per-turn usages), and the aggregate rate is the
// steadier, higher number compared to the volatile single-turn rate.
func TestSessionAggregateCacheRate(t *testing.T) {
	mock := &mockDeepSeek{t: t, reasoning: longReasoning}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 0, 0)
	const turns = 8
	for i := 0; i < turns; i++ {
		if err := a.Run(context.Background(), strings.Repeat("please consider this requirement. ", 6)); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}

	// The agent's cumulative counters must equal the sum of the per-turn usages.
	var sumHit, sumMiss int
	for _, u := range sink.usages {
		sumHit += u.CacheHitTokens
		sumMiss += u.CacheMissTokens
	}
	hit, miss := a.SessionCache()
	if hit != sumHit || miss != sumMiss {
		t.Errorf("SessionCache()=%d/%d but per-turn sums are %d/%d", hit, miss, sumHit, sumMiss)
	}

	agg := 100 * hit / (hit + miss)
	last := sink.usages[len(sink.usages)-1]
	single := 100 * last.CacheHitTokens / last.PromptTokens
	t.Logf("after %d turns: aggregate(session) = %d%%  vs  single(last turn) = %d%%", turns, agg, single)
	if agg <= 0 || agg > 100 {
		t.Errorf("aggregate rate out of range: %d%%", agg)
	}
}

func TestSetSessionResetsSessionCache(t *testing.T) {
	mock := &mockDeepSeek{t: t, reasoning: longReasoning}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, _ := newAgent(t, srv.URL, mock.tools(), 0, 0)
	if err := a.Run(context.Background(), strings.Repeat("please consider this requirement. ", 6)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	hit, miss := a.SessionCache()
	if hit+miss == 0 {
		t.Fatalf("SessionCache()=%d/%d before reset, want telemetry to record the turn", hit, miss)
	}
	a.SetSession(NewSession("system"))
	hit, miss = a.SessionCache()
	if hit != 0 || miss != 0 {
		t.Fatalf("SessionCache()=%d/%d after SetSession, want reset", hit, miss)
	}
}

func TestReleaseCacheHitGuard(t *testing.T) {
	if os.Getenv("REASONIX_RELEASE_CACHE_GUARD") == "" {
		t.Skip("set REASONIX_RELEASE_CACHE_GUARD=1 to run the release cache guard")
	}

	threshold := envInt("REASONIX_CACHE_GUARD_THRESHOLD", 90)
	maxLowCases := envInt("REASONIX_CACHE_GUARD_MAX_LOW_CASES", 1)

	cases := []struct {
		name string
		run  func(*testing.T) []int
	}{
		{
			name: "plain-dialogue",
			run: func(t *testing.T) []int {
				return cacheCurve(t, &mockDeepSeek{t: t, reasoning: longReasoning}, 14)
			},
		},
		{
			name: "plain-dialogue-no-reasoning",
			run: func(t *testing.T) []int {
				return cacheCurve(t, &mockDeepSeek{t: t}, 14)
			},
		},
		{
			name: "long-dialogue",
			run: func(t *testing.T) []int {
				return cacheCurveWithMessages(t, &mockDeepSeek{t: t, reasoning: longReasoning}, repeatedMessages(18, 18))
			},
		},
		{
			name: "mixed-message-sizes",
			run: func(t *testing.T) []int {
				msgs := make([]string, 0, 20)
				for i := 0; i < 20; i++ {
					repeats := 4
					if i%3 == 2 {
						repeats = 20
					}
					msgs = append(msgs, fmt.Sprintf("Turn %d: ", i)+strings.Repeat("preserve the request prefix while handling varied input. ", repeats))
				}
				return cacheCurveWithMessages(t, &mockDeepSeek{t: t, reasoning: longReasoning}, msgs)
			},
		},
		{
			name: "tool-loop",
			run: func(t *testing.T) []int {
				return toolLoopCurve(t, &mockDeepSeek{t: t, withTools: true, reasoning: longReasoning, toolRounds: 14})
			},
		},
		{
			name: "tool-loop-no-reasoning",
			run: func(t *testing.T) []int {
				return toolLoopCurve(t, &mockDeepSeek{t: t, withTools: true, toolRounds: 14})
			},
		},
		{
			name: "long-tool-loop",
			run: func(t *testing.T) []int {
				return toolLoopCurve(t, &mockDeepSeek{t: t, withTools: true, reasoning: longReasoning, toolRounds: 24})
			},
		},
		{
			name: "long-tool-loop-no-reasoning",
			run: func(t *testing.T) []int {
				return toolLoopCurve(t, &mockDeepSeek{t: t, withTools: true, toolRounds: 24})
			},
		},
	}

	type result struct {
		name string
		rate int
		all  []int
	}
	var lows []result
	for _, c := range cases {
		rates := c.run(t)
		rate := tailAverage(rates, 3)
		status := "pass"
		if rate < threshold {
			status = "low"
			lows = append(lows, result{name: c.name, rate: rate, all: rates})
		}
		t.Logf("CACHE_GUARD_RESULT: case=%s tail_avg=%d threshold=%d status=%s rates=%v",
			c.name, rate, threshold, status, rates)
	}

	if len(lows) > maxLowCases {
		var parts []string
		for _, low := range lows {
			parts = append(parts, fmt.Sprintf("%s=%d%%", low.name, low.rate))
		}
		msg := fmt.Sprintf("%d cache guard cases are below %d%%: %s", len(lows), threshold, strings.Join(parts, ", "))
		t.Logf("CACHE_GUARD_WARNING: %s", msg)
		if os.Getenv("REASONIX_CACHE_GUARD_STRICT") != "" {
			t.Fatal(msg)
		}
	}
}

func cacheCurve(t *testing.T, mock *mockDeepSeek, turns int) []int {
	return cacheCurveWithMessages(t, mock, repeatedMessages(turns, 6))
}

func repeatedMessages(turns, repeats int) []string {
	msgs := make([]string, 0, turns)
	for i := 0; i < turns; i++ {
		msgs = append(msgs, "Turn "+fmt.Sprint(i)+": "+strings.Repeat("please consider this requirement. ", repeats))
	}
	return msgs
}

func cacheCurveWithMessages(t *testing.T, mock *mockDeepSeek, messages []string) []int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 0, 0)
	for i, userMsg := range messages {
		if err := a.Run(context.Background(), userMsg); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}
	return usageRates(sink.usages)
}

func toolLoopCurve(t *testing.T, mock *mockDeepSeek) []int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	a, sink := newAgent(t, srv.URL, mock.tools(), 0, 0)
	if err := a.Run(context.Background(), strings.Repeat("please consider this requirement. ", 6)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return usageRates(sink.usages)
}

func usageRates(usages []*provider.Usage) []int {
	out := make([]int, len(usages))
	for i, u := range usages {
		out[i] = hitRate(u)
	}
	return out
}

func tailAverage(xs []int, n int) int {
	if len(xs) == 0 {
		return 0
	}
	if n > len(xs) {
		n = len(xs)
	}
	sum := 0
	for _, x := range xs[len(xs)-n:] {
		sum += x
	}
	return sum / n
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// newAgent wires a real openai.Provider at url into a real Agent.
func newAgent(t *testing.T, url string, reg *tool.Registry, contextWindow, recentKeep int) (*Agent, *collectSink) {
	t.Helper()
	prov, err := openai.New(provider.Config{
		Name:    "deepseek",
		BaseURL: url,
		Model:   "deepseek-reasoner",
		APIKey:  "test",
		Extra:   map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
	})
	if err != nil {
		t.Fatalf("provider New: %v", err)
	}
	sink := &collectSink{}
	a := New(prov, reg, NewSession(systemPrompt), Options{
		Temperature:   0,
		ContextWindow: contextWindow,
		RecentKeep:    recentKeep,
	}, sink)
	return a, sink
}

// --- request inspection helpers ---

func decodeMessages(body []byte) []json.RawMessage {
	var req struct {
		Messages []json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	return req.Messages
}

func isSummarizeRequest(body []byte) bool {
	msgs := decodeMessages(body)
	if len(msgs) == 0 {
		return false
	}
	var m struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(msgs[0], &m)
	return m.Role == "system" && strings.Contains(m.Content, "compacting the earlier part")
}

func commonPrefixMsgs(a, b []json.RawMessage) int {
	n := 0
	for n < len(a) && n < len(b) && bytes.Equal(a[n], b[n]) {
		n++
	}
	return n
}

func charsOf(msgs []json.RawMessage) int {
	total := 0
	for _, m := range msgs {
		total += len(m)
	}
	return total
}

// --- SSE chunk builders matching the streamResponse shape the provider parses ---

type sseDelta struct {
	Content          string        `json:"content,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []sseToolCall `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseResp struct {
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}

func deltaReasoning(s string) sseDelta { return sseDelta{ReasoningContent: s} }
func deltaText(s string) sseDelta      { return sseDelta{Content: s} }
func deltaToolCall(idx int, name, args string) sseDelta {
	tc := sseToolCall{Index: idx, ID: fmt.Sprintf("call_%d", idx), Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return sseDelta{ToolCalls: []sseToolCall{tc}}
}

func streamChunk(d sseDelta) sseResp { return sseResp{Choices: []sseChoice{{Delta: d}}} }
func finishChunk(reason string) sseResp {
	return sseResp{Choices: []sseChoice{{FinishReason: &reason}}}
}
func usageChunk(prompt, completion, hit, miss int) sseResp {
	return sseResp{Usage: &sseUsage{
		PromptTokens:          prompt,
		CompletionTokens:      completion,
		TotalTokens:           prompt + completion,
		PromptCacheHitTokens:  hit,
		PromptCacheMissTokens: miss,
	}}
}

func writeSSE(w http.ResponseWriter, t *testing.T, chunks ...sseResp) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("ResponseWriter is not a Flusher")
	}
	for _, c := range chunks {
		b, _ := json.Marshal(c)
		fmt.Fprintf(w, "data: %s\n\n", b)
		f.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	f.Flush()
}
