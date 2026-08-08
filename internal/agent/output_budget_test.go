package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// sharedWindowBudgetProvider simulates DeepSeek: a large default output
// budget that shares the context window with the prompt input.
type sharedWindowBudgetProvider struct {
	budget int
}

func (*sharedWindowBudgetProvider) Name() string { return "shared-window" }
func (p *sharedWindowBudgetProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}
func (p *sharedWindowBudgetProvider) OutputBudget() int       { return p.budget }
func (*sharedWindowBudgetProvider) SharesContextWindow() bool { return true }

var _ provider.Provider = (*sharedWindowBudgetProvider)(nil)
var _ provider.OutputBudgetProvider = (*sharedWindowBudgetProvider)(nil)
var _ provider.SharedWindowOutputProvider = (*sharedWindowBudgetProvider)(nil)

// independentBudgetProvider simulates MiMo/OpenAI: a large output budget that
// does NOT compete with the prompt input.
type independentBudgetProvider struct {
	budget int
}

func (*independentBudgetProvider) Name() string { return "independent" }
func (p *independentBudgetProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}
func (p *independentBudgetProvider) OutputBudget() int { return p.budget }

var _ provider.Provider = (*independentBudgetProvider)(nil)
var _ provider.OutputBudgetProvider = (*independentBudgetProvider)(nil)

// sharedWindowOnlyProvider implements SharesContextWindow but no output budget.
type sharedWindowOnlyProvider struct{}

func (*sharedWindowOnlyProvider) Name() string { return "shared-no-budget" }
func (*sharedWindowOnlyProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}
func (*sharedWindowOnlyProvider) SharesContextWindow() bool { return true }

var _ provider.Provider = (*sharedWindowOnlyProvider)(nil)
var _ provider.SharedWindowOutputProvider = (*sharedWindowOnlyProvider)(nil)

func TestEffectiveOutputBudgetSmallInputKeepsDefault(t *testing.T) {
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
	}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "short prompt"}}
	clipped, ok := a.effectiveOutputBudget(msgs)
	if ok {
		t.Fatalf("small input must keep the default budget, got clipped=%d", clipped)
	}
}

func TestEffectiveOutputBudgetClipsNearWindow(t *testing.T) {
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
	}
	// Simulate ~1.0M-token input: only a small allowance remains for output.
	msgs := make([]provider.Message, 0, 20)
	for i := 0; i < 20; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: bigTokenString(50_000)})
	}
	clipped, ok := a.effectiveOutputBudget(msgs)
	if !ok {
		t.Fatal("near-window input must clip the output budget")
	}
	if clipped >= 128*1024 {
		t.Fatalf("clipped budget %d must be below the default 131072", clipped)
	}
	if clipped <= 0 {
		t.Fatalf("clipped budget %d must stay positive", clipped)
	}
}

func TestEffectiveOutputBudgetFloorNearExhaustion(t *testing.T) {
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
	}
	// Input that nearly exhausts the window: floor must still allow output.
	msgs := make([]provider.Message, 0, 20)
	for i := 0; i < 20; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: bigTokenString(60_000)})
	}
	clipped, ok := a.effectiveOutputBudget(msgs)
	if !ok {
		t.Fatal("near-exhaustion input must clip")
	}
	if clipped < minOutputBudget {
		t.Fatalf("clipped budget %d below floor %d", clipped, minOutputBudget)
	}
}

func TestEffectiveOutputBudgetIndependentVendorUnchanged(t *testing.T) {
	a := &Agent{
		prov:          &independentBudgetProvider{budget: 128 * 1024},
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
	}
	msgs := make([]provider.Message, 0, 20)
	for i := 0; i < 20; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: bigTokenString(50_000)})
	}
	if clipped, ok := a.effectiveOutputBudget(msgs); ok {
		t.Fatalf("independent-ceiling vendor must never clip, got %d", clipped)
	}
}

func TestEffectiveOutputBudgetNoBudgetNoWindow(t *testing.T) {
	// No output budget exposed: nothing to clip.
	if clipped, ok := (&Agent{prov: &sharedWindowOnlyProvider{}, contextWindow: 1_048_576}).effectiveOutputBudget(nil); ok {
		t.Fatalf("no budget must not clip, got %d", clipped)
	}
	// No context window: unknown limit, keep default.
	if clipped, ok := (&Agent{prov: &sharedWindowBudgetProvider{budget: 128 * 1024}}).effectiveOutputBudget(nil); ok {
		t.Fatalf("no window must not clip, got %d", clipped)
	}
}

// sharedFakeProvider embeds fakeProvider (which can summarize via its Stream
// reply) and advertises a shared context window like DeepSeek.
type sharedFakeProvider struct {
	*fakeProvider
	budget int
}

func (p *sharedFakeProvider) OutputBudget() int       { return p.budget }
func (*sharedFakeProvider) SharesContextWindow() bool { return true }

var _ provider.Provider = (*sharedFakeProvider)(nil)
var _ provider.OutputBudgetProvider = (*sharedFakeProvider)(nil)
var _ provider.SharedWindowOutputProvider = (*sharedFakeProvider)(nil)

// bigTokenString returns a string that estimates to roughly n tokens under
// estimateTextTokens (CJK-heavy: ~1 rune per token).
func bigTokenString(n int) string {
	const rune = "字"
	b := make([]byte, 0, n*3)
	for len(b)/3 < n {
		b = append(b, rune...)
	}
	return string(b)
}

func newSessionWithMsgs(msgs []provider.Message) *Session {
	s := NewSession("")
	for _, m := range msgs {
		s.Add(m)
	}
	return s
}

func TestMaybeCompactOnResumeUnsharedWindowNoop(t *testing.T) {
	a := &Agent{prov: &independentBudgetProvider{budget: 128 * 1024}, contextWindow: 1_048_576, sink: event.Discard}
	a.session = newSessionWithMsgs([]provider.Message{{Role: provider.RoleUser, Content: "x"}})
	a.MaybeCompactOnResume(context.Background())
	if st := a.compactionState; len(st.Projection.Messages) != 0 {
		t.Fatal("independent vendor must not compact on resume")
	}
}

func TestMaybeCompactOnResumeOversizedPromptCompacts(t *testing.T) {
	fp := &fakeProvider{reply: "GOAL: fit inside window"}
	sess := NewSession("sys")
	for i := 0; i < 40; i++ {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "user turn " + strings.Repeat("x", 200)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant " + strings.Repeat("y", 400)})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	// Tiny window so the oversized prompt exceeds window - minBudget - reserve.
	a := New(fp, nil, sess, Options{
		ContextWindow: 30_000,
		RecentKeep:    2,
		ArchiveDir:    dir,
		SessionPath:   path,
		ModelRef:      "test/model",
	}, event.Discard)
	// Shared-window vendor: fakeProvider must advertise it for the gate to run.
	// Re-point the agent's provider to one that shares the window and can
	// summarize (fakeProvider streams the reply text).
	a.prov = &sharedFakeProvider{fakeProvider: fp, budget: 128 * 1024}
	a.outputBudget = 128 * 1024

	a.MaybeCompactOnResume(context.Background())
	if len(a.compactionState.Projection.Messages) == 0 {
		t.Fatal("oversized prompt must install a projection on resume")
	}
}

func TestMaybeCompactOnResumeColdLargePromptUntouched(t *testing.T) {
	// Deferred compaction: a cold cache with a large-but-fitting prompt is left
	// untouched — replay pays the miss price once, cheaper than rewriting the
	// prefix on every resume. Only an input-overflow (would-400) prompt compacts.
	fp := &fakeProvider{reply: "GOAL: cold replay avoided"}
	sess := NewSession("sys")
	for i := 0; i < 20; i++ {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "user turn " + strings.Repeat("x", 200)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant " + strings.Repeat("y", 400)})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	a := New(fp, nil, sess, Options{
		ContextWindow: 100_000,
		RecentKeep:    2,
		ArchiveDir:    dir,
		SessionPath:   path,
		ModelRef:      "test/model",
	}, event.Discard)
	a.prov = &sharedFakeProvider{fakeProvider: fp, budget: 128 * 1024}
	a.outputBudget = 128 * 1024
	a.cacheState = CacheStateCold

	a.MaybeCompactOnResume(context.Background())
	if st := a.compactionState; len(st.Projection.Messages) != 0 {
		t.Fatal("cold cache + fitting prompt must NOT compact on resume (deferred policy)")
	}
}

func TestMaybeCompactOnResumeWarmSmallPromptUntouched(t *testing.T) {
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
		cacheState:    CacheStateWarm,
		sink:          event.Discard,
	}
	a.session = newSessionWithMsgs([]provider.Message{{Role: provider.RoleUser, Content: "short"}})
	a.MaybeCompactOnResume(context.Background())
	if st := a.compactionState; len(st.Projection.Messages) != 0 {
		t.Fatal("warm small resume must keep the cached prefix untouched")
	}
}

// TestMaybeCompactOnResumeWarmMidPromptUntouched guards the resume gate against
// the overflow-guard safety factor: the real-shape estimate (~49% of the
// window) must NOT be doubled into an apparent ~98% that folds a warm cached
// prefix. Over-estimating here destroys the server prefix cache of a healthy
// session; under-estimating only defers compaction to the request path.
func TestMaybeCompactOnResumeWarmMidPromptUntouched(t *testing.T) {
	sink := &recordSink{}
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 100_000,
		outputBudget:  128 * 1024,
		cacheState:    CacheStateWarm,
		sink:          sink,
	}
	// ~49K tokens real shape (ASCII: 1 rune ≈ 1 token), well inside the
	// 100K - 8K - 8K = 83.6K resume threshold.
	a.session = newSessionWithMsgs([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("a", 49_000)},
	})
	a.MaybeCompactOnResume(context.Background())
	if got := len(sink.kinds(event.CompactionStarted)); got != 0 {
		t.Fatalf("warm resume at ~49%% of the window compacted (%d starts), want none", got)
	}
}

// TestMaybeCompactOnResumeProjectionSmallCanonicalHugeUntouched guards the
// resume gate against the canonical transcript: a valid projection plus tail
// is the exact sent shape, so a huge canonical history behind a small
// projection must NOT trigger the gate (Copilot review 8/8: "estimate the
// model-visible messages instead").
func TestMaybeCompactOnResumeProjectionSmallCanonicalHugeUntouched(t *testing.T) {
	sink := &recordSink{}
	a := &Agent{
		prov:          &sharedWindowBudgetProvider{budget: 128 * 1024},
		contextWindow: 100_000,
		outputBudget:  128 * 1024,
		cacheState:    CacheStateWarm,
		sink:          sink,
		workspaceID:   "ws",
		sessionPath:   "sess.jsonl",
		modelRef:      "model",
	}
	// Canonical history alone (~90K ASCII) exceeds the 100K - 8K - 8K gate.
	canonical := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("a", 90_000)},
	}
	a.session = newSessionWithMsgs(canonical)
	msgs, version := a.session.snapshotMessagesVersion()
	// A valid projection keeps the model-visible shape tiny.
	a.compactionState = CompactionState{
		TranscriptVersion: version,
		PromptCacheKey:    "ws|sess|model",
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "summary"},
			},
			TranscriptVersion: version,
			CoveredCount:      len(msgs),
			CoveredPrefixHash: coveredPrefixHash(msgs, len(msgs)),
		},
	}
	a.MaybeCompactOnResume(context.Background())
	if got := len(sink.kinds(event.CompactionStarted)); got != 0 {
		t.Fatalf("resume with a small valid projection compacted (%d starts), want none", got)
	}
}

// capturingBudgetProvider embeds sharedFakeProvider and records the MaxTokens
// of the last streamed request plus how many streams ran.
type capturingBudgetProvider struct {
	sharedFakeProvider
	maxTokens int
	streams   int
}

func (p *capturingBudgetProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.maxTokens = req.MaxTokens
	p.streams++
	return p.sharedFakeProvider.Stream(ctx, req)
}

var _ provider.Provider = (*capturingBudgetProvider)(nil)
var _ provider.OutputBudgetProvider = (*capturingBudgetProvider)(nil)
var _ provider.SharedWindowOutputProvider = (*capturingBudgetProvider)(nil)

// TestSummarizeClipsSharedWindowBudget verifies the compaction summarizer clips
// its own MaxTokens for a shared-window vendor: an unclipped default made
// compaction itself fail with HTTP 400 near the window edge.
func TestSummarizeClipsSharedWindowBudget(t *testing.T) {
	cap := &capturingBudgetProvider{}
	cap.fakeProvider = &fakeProvider{reply: "SUMMARY"}
	cap.budget = 128 * 1024
	a := &Agent{
		prov:          cap,
		contextWindow: 1_048_576,
		outputBudget:  128 * 1024,
		sink:          event.Discard,
	}
	// Region near the window edge: input alone estimates past the allowance.
	region := []provider.Message{{Role: provider.RoleUser, Content: bigTokenString(900_000)}}
	summary, _, err := a.summarize(context.Background(), region, "")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary != "SUMMARY" {
		t.Fatalf("summary = %q, want %q", summary, "SUMMARY")
	}
	if cap.maxTokens == 0 || cap.maxTokens >= 128*1024 {
		t.Fatalf("MaxTokens = %d, want a clipped value under the 128K default", cap.maxTokens)
	}
}

// TestSummarizeRejectsTooLargeInput verifies the summarizer refuses a fold
// that cannot fit beside a usable output budget: the request would 400 again
// and retrying it every turn just re-runs the failure.
func TestSummarizeRejectsTooLargeInput(t *testing.T) {
	cap := &capturingBudgetProvider{}
	cap.fakeProvider = &fakeProvider{reply: "SUMMARY"}
	cap.budget = 128 * 1024
	a := &Agent{
		prov:          cap,
		contextWindow: 500_000,
		outputBudget:  128 * 1024,
		sink:          event.Discard,
	}
	// Fold larger than window - minOutputBudget: the rendered transcript
	// estimates ~500K tokens.
	region := []provider.Message{{Role: provider.RoleUser, Content: bigTokenString(500_000)}}
	_, _, err := a.summarize(context.Background(), region, "")
	if !errors.Is(err, ErrCompactionInputTooLarge) {
		t.Fatalf("summarize err = %v, want ErrCompactionInputTooLarge", err)
	}
	if cap.streams != 0 {
		t.Fatalf("summarize streamed %d requests; a too-large fold must be rejected before sending", cap.streams)
	}
}

// TestMaybeCompactPausesOnTooLargeInput verifies auto-compaction stops retrying
// when the fold cannot fit the window: compactStuck latches and a warn notice
// explains why, instead of re-running a doomed summarize every turn.
func TestMaybeCompactPausesOnTooLargeInput(t *testing.T) {
	cap := &capturingBudgetProvider{}
	cap.fakeProvider = &fakeProvider{reply: "SUMMARY"}
	cap.budget = 128 * 1024
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: bigTokenString(500_000)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	a := &Agent{
		prov:              cap,
		contextWindow:     500_000,
		outputBudget:      128 * 1024,
		compactRatio:      0.8,
		compactForceRatio: 0.9,
		sink:              sink,
	}
	a.session = sess
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 500_000})
	if !a.compactStuck {
		t.Fatal("too-large fold must latch compactStuck to stop auto-retry")
	}
	found := false
	for _, n := range notices {
		if strings.Contains(n, "too large to compact") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'too large to compact' notice, got %v", notices)
	}
}

// TestMaybePredictOverflowFires verifies a record-only notice when the
// estimated prompt + max output leaves less than 8K of headroom.
func TestMaybePredictOverflowFires(t *testing.T) {
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})
	a := &Agent{
		contextWindow: 1_048_576,
		sink:          sink,
	}
	// est 1M, max 128K → headroom = 1M - 1M - 128K = -128K < 8K → fires.
	a.maybePredictOverflow(1_048_576, 131_072)
	found := false
	for _, n := range notices {
		if n == "context window nearly full" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected overflow notice, got %v", notices)
	}
}

// TestMaybePredictOverflowDoesNotFire when there is adequate headroom.
func TestMaybePredictOverflowDoesNotFire(t *testing.T) {
	count := 0
	sink := event.FuncSink(func(e event.Event) {
		count++
	})
	a := &Agent{
		contextWindow: 1_048_576,
		sink:          sink,
	}
	// est 100K, max 128K → headroom = 1M - 100K - 128K = ~820K >> 8K → no fire.
	a.maybePredictOverflow(100_000, 131_072)
	if count > 0 {
		t.Fatalf("overflow predicted on a small prompt (%d events)", count)
	}
}
