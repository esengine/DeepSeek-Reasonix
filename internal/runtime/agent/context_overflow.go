package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

// contextWindowProbe is what an endpoint that declares no window still tells us
// about it: the smallest request it refused as too long, and the largest it
// accepted. Both are sizes of requests we actually sent, so together they bound
// the real window instead of guessing at one.
type contextWindowProbe struct {
	rejected atomic.Int64
	accepted atomic.Int64
}

func (p *contextWindowProbe) noteRejected(tokens int) {
	if p == nil || tokens <= 0 {
		return
	}
	for {
		current := p.rejected.Load()
		if current > 0 && current <= int64(tokens) {
			return
		}
		if p.rejected.CompareAndSwap(current, int64(tokens)) {
			return
		}
	}
}

func (p *contextWindowProbe) noteAccepted(tokens int) {
	if p == nil || tokens <= 0 {
		return
	}
	for {
		current := p.accepted.Load()
		if current >= int64(tokens) {
			return
		}
		if p.accepted.CompareAndSwap(current, int64(tokens)) {
			return
		}
	}
}

// window is the size to budget against, or zero while nothing has been
// rejected: a request that fit says nothing about where the ceiling is. Once
// one has, the largest accepted size is preferred — it is the only bound known
// to fit — and the refused size stands in until such a request exists.
func (p *contextWindowProbe) window() int {
	if p == nil {
		return 0
	}
	rejected := p.rejected.Load()
	if rejected <= 0 {
		return 0
	}
	// A refusal too small for one fold to be worth summarizing is not a window
	// this agent can work inside: adopting it would put every later turn under a
	// ceiling no projection can meet.
	if rejected < minSummarySpanTokens {
		return 0
	}
	if accepted := p.accepted.Load(); accepted > 0 && accepted < rejected {
		return int(accepted)
	}
	return int(rejected)
}

// effectiveContextWindow is the window every context budget is taken from: the
// configured one, or the bound probed from a rejection when no window was
// declared. Zero still means no window is known, which leaves compaction off.
func (a *contextWindow) effectiveContextWindow() int {
	if a == nil {
		return 0
	}
	if a.contextWindow > 0 {
		return a.contextWindow
	}
	return a.windowProbe.window()
}

// noteAcceptedPromptTokens records a provider-counted prompt size as a lower
// bound on the window. It applies the same trust rule as prompt calibration: an
// estimate is our own arithmetic, and a turn whose provider ran its own tools
// was billed for pages we never sent.
func (a *contextWindow) noteAcceptedPromptTokens(usage *provider.Usage) {
	if a == nil || usage == nil || usage.Estimated || usage.ServerToolRequests > 0 {
		return
	}
	a.windowProbe.noteAccepted(usage.LatestPromptTokens())
}

// recoverContextOverflow answers a provider's "this input is too long" by
// folding the context and rebuilding the frozen request, so the round can be
// replayed instead of failing the turn. It reports false when the fold freed
// nothing, because resending a request the provider already refused only spends
// another call to be told the same thing.
func (a *contextWindow) recoverContextOverflow(ctx context.Context, frozen *samplingRequest, err error) bool {
	if a == nil || frozen == nil || frozen.overflowFolded || !provider.IsContextOverflow(err) {
		return false
	}
	frozen.overflowFolded = true
	a.windowProbe.noteRejected(a.estimatedRequestTokens(frozen.req))
	refused := sessionstore.ProviderVisibleFingerprint(provider.ModelMessages(frozen.req.Messages))
	if _, prepareErr := a.contextManager().Prepare(ctx, ContextPreparePolicy{
		Trigger: CompactionTriggerOverflow,
	}); prepareErr != nil {
		return false
	}
	rebuilt, buildErr := a.prepareSamplingRequest(ctx)
	if buildErr != nil {
		return false
	}
	if sessionstore.ProviderVisibleFingerprint(provider.ModelMessages(rebuilt.req.Messages)) == refused {
		return false
	}
	frozen.req = rebuilt.req
	return true
}
