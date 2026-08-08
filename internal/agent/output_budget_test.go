package agent

import (
	"context"
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
