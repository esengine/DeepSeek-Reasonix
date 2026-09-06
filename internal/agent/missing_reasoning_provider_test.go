package agent

import (
	"context"
	"errors"
	"sync/atomic"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/provider"
)

type toolCallReasoningRequiredProvider struct{ *testutil.MockProvider }

func (p toolCallReasoningRequiredProvider) RequiresToolCallReasoning() bool    { return true }
func (p toolCallReasoningRequiredProvider) AllowsEmptyReasoningFallback() bool { return true }

type configuredToolCallReasoningProvider struct {
	*testutil.MockProvider
	identity string
}

func (p configuredToolCallReasoningProvider) RequiresToolCallReasoning() bool    { return true }
func (p configuredToolCallReasoningProvider) AllowsEmptyReasoningFallback() bool { return false }
func (p configuredToolCallReasoningProvider) MissingToolCallReasoningWarningIdentity() string {
	return p.identity
}

type cancelMissingReasoningRetryProvider struct {
	calls          atomic.Int32
	retryUsageSent chan struct{}
}

func (p *cancelMissingReasoningRetryProvider) Name() string { return "deepseek-cancel-retry" }
func (p *cancelMissingReasoningRetryProvider) RequiresToolCallReasoning() bool {
	return true
}
func (p *cancelMissingReasoningRetryProvider) AllowsEmptyReasoningFallback() bool { return false }

func (p *cancelMissingReasoningRetryProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	call := p.calls.Add(1)
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		send := func(chunk provider.Chunk) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- chunk:
				return true
			}
		}
		if call == 1 {
			toolCall := provider.ToolCall{ID: "discarded", Name: "echo", Arguments: `{"text":"must not run"}`}
			if !send(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &toolCall}) {
				return
			}
			if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}}) {
				return
			}
			send(provider.Chunk{Type: provider.ChunkDone})
			return
		}
		if !send(provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}}) {
			return
		}
		close(p.retryUsageSent)
		<-ctx.Done()
	}()
	return ch, nil
}

type strictToolCallReasoningProvider struct{ *testutil.MockProvider }

func (strictToolCallReasoningProvider) RequiresToolCallReasoning() bool    { return true }
func (strictToolCallReasoningProvider) AllowsEmptyReasoningFallback() bool { return false }

func isReplayFailureForTest(err error) bool {
	var target *ReasoningReplayError
	return errors.As(err, &target)
}
