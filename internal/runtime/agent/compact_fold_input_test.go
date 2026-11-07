package agent

import (
	"context"
	"fmt"

	"reasonix/internal/contract/provider"
)

// countingProvider records every summarizer call so tests can assert that a
// fold costs exactly one request.
type countingProvider struct {
	reply string
	got   []provider.Request
}

func (p *countingProvider) Name() string { return "counting" }

func (p *countingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.got = append(p.got, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf("%s %d", p.reply, len(p.got))}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
