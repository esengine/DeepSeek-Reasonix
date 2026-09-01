package completioneval

import (
	"context"
	"fmt"
	"sync"
	"time"

	"reasonix/internal/boundedllm"
	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

// Evaluator is the host-facing interface the agent layer consumes. A nil or
// failing evaluator is a fail-closed signal: the host must treat the candidate
// termination as unconfirmed rather than silently succeeding.
type Evaluator interface {
	// Evaluate runs one bounded validation. Any error (timeout, stream
	// failure, invalid JSON, over-budget evidence) means the candidate could
	// not be confirmed.
	Evaluate(ctx context.Context, evidence Evidence) (Verdict, error)
}

// Session is a bounded completion validator that calls provider.Stream
// directly. It deliberately has no tools, no session history, and no
// compaction, and it never touches the main conversation's
// previous_response_id or tool schemas. Each agent (root, planner, subagent)
// holds its own Session so concurrent agents never serialize behind one
// shared mutex.
type Session struct {
	prov     provider.Provider
	pricing  *provider.Pricing
	modelRef string
	sink     event.Sink
	timeout  time.Duration

	mu sync.Mutex // serializes evaluations sharing this session's provider call path
}

// NewSession creates a completion validator with temperature 0 and MaxTokens
// 256. prov is normally the same provider/model the working agent uses, so
// evidence never implicitly crosses to another model; modelRef labels emitted
// usage. sink may be nil.
func NewSession(prov provider.Provider, pricing *provider.Pricing, modelRef string, sink event.Sink) *Session {
	return &Session{
		prov:     prov,
		pricing:  pricing,
		modelRef: modelRef,
		sink:     sink,
		timeout:  Timeout,
	}
}

// Evaluate implements Evaluator.
func (s *Session) Evaluate(ctx context.Context, evidence Evidence) (Verdict, error) {
	if s == nil || nilutil.IsNil(s.prov) {
		return Verdict{}, fmt.Errorf("completion evaluator unavailable")
	}
	if nilutil.IsNil(ctx) {
		ctx = context.Background()
	}
	payload, err := buildEvidence(evidence)
	if err != nil {
		return Verdict{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	text, err := boundedllm.Call(ctx, boundedllm.Config{
		Provider:       s.prov,
		Pricing:        s.pricing,
		ModelRef:       s.modelRef,
		Sink:           s.sink,
		UsageSource:    event.UsageSourceCompletionEvaluator,
		Timeout:        s.timeout,
		MaxTokens:      MaxTokens,
		MaxOutputBytes: MaxOutputBytes,
		MaxSystemBytes: boundedllm.DefaultMaxSystemBytes,
		MaxTotalBytes:  boundedllm.DefaultMaxTotalBytes,
	}, PolicyPrompt, payload)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(text)
}
