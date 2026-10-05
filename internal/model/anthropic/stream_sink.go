package anthropic

import (
	"context"

	"reasonix/internal/contract/provider"
)

// chunkSink delivers stream chunks and remembers whether any reached the
// consumer, which decides whether a failed attempt may be made again.
type chunkSink struct {
	ctx     context.Context
	out     chan<- provider.Chunk
	emitted bool
}

func (s *chunkSink) send(chunk provider.Chunk) bool {
	select {
	case s.out <- chunk:
		s.emitted = true
		return true
	default:
	}
	select {
	case <-s.ctx.Done():
		return false
	case s.out <- chunk:
		s.emitted = true
		return true
	}
}
