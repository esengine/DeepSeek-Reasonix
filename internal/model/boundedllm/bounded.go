// Package boundedllm provides the shared bounded no-tool provider call
// infrastructure used by independent host reviewers (the Auto Guard recovery
// reviewer and the Goal evaluator). Each reviewer is deliberately isolated from
// the main conversation: no tools, no session history, no compaction — a single
// temperature-0 request with hard time/output budgets whose usage is attributed
// to the reviewer's own source, never the main session's prompt cache.
package boundedllm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const (
	// DefaultTimeout bounds one reviewer request.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxTokens caps the model's completion length.
	DefaultMaxTokens = 256
	// DefaultMaxOutputBytes aborts the stream if the provider ignores MaxTokens.
	DefaultMaxOutputBytes = 4 * 1024
	// DefaultMaxSystemBytes caps the fixed system policy.
	DefaultMaxSystemBytes = 2 * 1024
	// DefaultMaxTotalBytes caps system + evidence together; each caller budgets
	// its own evidence below this.
	DefaultMaxTotalBytes = 8 * 1024
)

// Config carries one bounded reviewer call's policy and accounting hooks.
type Config struct {
	// Provider is the model endpoint. Required.
	Provider provider.Provider
	// Pricing is used only for usage cost display; nil omits cost.
	Pricing *provider.Pricing
	// ModelRef is the canonical "provider/model" label on emitted usage events.
	ModelRef string
	// Sink receives the billable Usage event; nil disables emission.
	Sink event.Sink
	// UsageSource labels the emitted usage (e.g. event.UsageSourceGoalEvaluator).
	// Empty means no Usage event is emitted.
	UsageSource string
	// Timeout bounds the whole call. Zero uses DefaultTimeout.
	Timeout time.Duration
	// MaxTokens caps the completion. Zero uses DefaultMaxTokens.
	MaxTokens int
	// MaxOutputBytes aborts the stream once exceeded. Zero uses DefaultMaxOutputBytes.
	MaxOutputBytes int
	// MaxSystemBytes is the hard cap on the fixed system policy. Zero uses DefaultMaxSystemBytes.
	MaxSystemBytes int
	// MaxTotalBytes is the hard cap on system + evidence. Zero uses DefaultMaxTotalBytes.
	MaxTotalBytes int
	// ReasoningAnswer returns the reasoning stream when a stop left content empty;
	// only for callers whose contract extracts its answer from structure.
	ReasoningAnswer bool
}

// Call runs one bounded no-tool request: system policy + a single user evidence
// message, temperature 0, capped completion, and streamed output collected up to
// MaxOutputBytes. It returns the raw response text (the caller parses its own
// JSON contract). Usage is emitted to Sink under UsageSource when both are set.
func Call(ctx context.Context, cfg Config, system, evidence string) (string, error) {
	if nilutil.IsNil(cfg.Provider) {
		return "", fmt.Errorf("bounded reviewer provider unavailable")
	}
	if nilutil.IsNil(ctx) {
		ctx = context.Background()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	callCtx = provider.WithRequestAttemptCounter(callCtx)

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	maxOutputBytes := cfg.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = DefaultMaxOutputBytes
	}
	maxSystemBytes := cfg.MaxSystemBytes
	if maxSystemBytes <= 0 {
		maxSystemBytes = DefaultMaxSystemBytes
	}
	maxTotalBytes := cfg.MaxTotalBytes
	if maxTotalBytes <= 0 {
		maxTotalBytes = DefaultMaxTotalBytes
	}
	if len(system) > maxSystemBytes {
		// Should never happen; keep fail-closed if a policy grows past budget.
		return "", fmt.Errorf("bounded reviewer system policy exceeds %d bytes", maxSystemBytes)
	}
	if len(system)+len(evidence) > maxTotalBytes {
		// Must not mid-clip JSON. Evidence is field-budgeted by the caller;
		// remaining overflow can only come from policy growth — fail closed.
		return "", fmt.Errorf("bounded reviewer request exceeds %d bytes", maxTotalBytes)
	}

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: system},
			{Role: provider.RoleUser, Content: evidence},
		},
		// No tools.
		Temperature: provider.TemperaturePtr(0),
		MaxTokens:   maxTokens,
	}

	var usage *provider.Usage
	defer func() {
		usage = provider.UsageWithRequestAttemptCount(callCtx, usage)
		if usage != nil && cfg.UsageSource != "" && cfg.Sink != nil {
			cfg.Sink.Emit(event.Event{
				Kind:        event.Usage,
				ModelRef:    cfg.ModelRef,
				Usage:       usage,
				Pricing:     cfg.Pricing,
				UsageSource: cfg.UsageSource,
				Source:      cfg.UsageSource,
			})
		}
	}()

	ch, err := cfg.Provider.Stream(callCtx, req)
	if err != nil {
		return "", err
	}

	var text strings.Builder
	reasoning := reasoningAnswer{keep: cfg.ReasoningAnswer, limit: maxOutputBytes}
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			if text.Len() > maxOutputBytes {
				cancel()
				return "", fmt.Errorf("bounded reviewer output exceeded %d bytes", maxOutputBytes)
			}
		case provider.ChunkReasoning:
			reasoning.add(chunk.Text)
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				u := *chunk.Usage
				usage = &u
			}
		case provider.ChunkError:
			if chunk.Err != nil {
				return "", chunk.Err
			}
			return "", fmt.Errorf("bounded reviewer stream error")
		}
	}
	if callCtx.Err() != nil && text.Len() == 0 {
		return "", callCtx.Err()
	}
	if text.Len() == 0 {
		return reasoning.answer(usage)
	}
	return text.String(), nil
}

// reasoningAnswer holds the reasoning stream of a caller that accepts it as the
// answer. It is charged against the output budget only when it becomes one, so
// a model that reasons at length before writing content is not refused.
type reasoningAnswer struct {
	keep     bool
	limit    int
	text     strings.Builder
	overflow bool
}

func (r *reasoningAnswer) add(s string) {
	if !r.keep || r.overflow {
		return
	}
	r.text.WriteString(s)
	if r.text.Len() > r.limit {
		r.overflow = true
		r.text.Reset()
	}
}

// answer is the reasoning when the provider reported a clean stop; a truncated
// stream's reasoning is unfinished thought, not an answer.
func (r *reasoningAnswer) answer(usage *provider.Usage) (string, error) {
	if !r.keep || usage == nil || usage.FinishReason != "stop" {
		return "", nil
	}
	if r.overflow {
		return "", fmt.Errorf("bounded reviewer reasoning exceeded %d bytes", r.limit)
	}
	return r.text.String(), nil
}
