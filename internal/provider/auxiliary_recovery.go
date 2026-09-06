package provider

import (
	"context"
	"errors"
	"time"
)

// StreamAuxiliary applies the finite policy to search and summarization. It
// buffers attempts so failed partial summaries never leak into their caller.
func StreamAuxiliary(ctx context.Context, p Provider, req Request) (<-chan Chunk, error) {
	ctx = WithManagedRecovery(WithIndependentRequestAttemptCounter(ctx))
	out := make(chan Chunk)
	var aggregate Usage
	go func() {
		defer close(out)
		send := func(c Chunk) bool {
			select {
			case out <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for attempt := range 4 {
			attemptCtx, cancel := context.WithCancel(ctx)
			ch, err := p.Stream(attemptCtx, req)
			var latest *Usage
			var chunks []Chunk
			complete := false
			bytes := 0
			if err == nil {
			loop:
				for {
					select {
					case <-ctx.Done():
						cancel()
						return
					case c, ok := <-ch:
						if !ok {
							break loop
						}
						if c.Type == ChunkError {
							err = c.Err
							if err == nil {
								err = errors.New("auxiliary provider error")
							}
							break loop
						}
						bytes += len(c.Text)
						if bytes > 16*1024*1024 {
							err = errors.New("auxiliary response exceeds local limit")
							break loop
						}
						if c.Type == ChunkUsage {
							latest = c.Usage
							continue
						}
						complete = complete || c.Type == ChunkDone
						chunks = append(chunks, c)
					}
				}
				if err == nil && !complete {
					err = StreamInterrupt(errors.New("auxiliary response ended before terminal event"), "unexpected_eof")
				}
			}
			cancel()
			if latest == nil {
				aggregate.Unknown = true
			}
			if latest != nil {
				aggregate.PromptTokens += latest.PromptTokens
				aggregate.CompletionTokens += latest.CompletionTokens
				aggregate.TotalTokens += latest.TotalTokens
				aggregate.CacheHitTokens += latest.CacheHitTokens
				aggregate.CacheMissTokens += latest.CacheMissTokens
				aggregate.ReasoningTokens += latest.ReasoningTokens
				aggregate.CacheWriteTokens += latest.CacheWriteTokens
				aggregate.CacheWriteBilledTokens += latest.CacheWriteBilledTokens
				aggregate.FinishReason = latest.FinishReason
				aggregate.Estimated = aggregate.Estimated || latest.Estimated
				aggregate.Unknown = aggregate.Unknown || latest.Unknown
			}
			aggregate.RequestCount = RequestAttemptCount(ctx)
			if aggregate.RequestCount == 0 {
				aggregate.RequestCount = attempt + 1
			}
			if err == nil {
				send(Chunk{Type: ChunkUsage, Usage: &aggregate})
				for _, c := range chunks {
					if !send(c) {
						return
					}
				}
				return
			}
			f := ClassifyRecovery(err)
			if !f.Retryable || attempt == 3 {
				send(Chunk{Type: ChunkUsage, Usage: &aggregate})
				send(Chunk{Type: ChunkError, Err: err})
				return
			}
			delay := time.Duration(1<<attempt) * 2 * time.Second
			delay = max(delay, f.RetryAfter)
			if !auxiliarySleep(ctx, delay) {
				return
			}
		}
	}()
	return out, nil
}

type recoverySleeperKey struct{}

// WithRecoverySleeper supplies the owner clock without changing the retry policy.
func WithRecoverySleeper(ctx context.Context, sleep func(context.Context, time.Duration) bool) context.Context {
	return context.WithValue(ctx, recoverySleeperKey{}, sleep)
}
func auxiliarySleep(ctx context.Context, d time.Duration) bool {
	if sleep, ok := ctx.Value(recoverySleeperKey{}).(func(context.Context, time.Duration) bool); ok {
		return sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
