//go:build linux

package boot

import (
	"context"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

type verifiedRootProvider struct {
	calls    atomic.Int32
	existing bool
}

func (*verifiedRootProvider) Name() string { return "verified-root" }

func (p *verifiedRootProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := p.calls.Add(1)
	if p.existing {
		call--
	}
	switch call {
	case 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "read", Name: "read_file", Arguments: `{"path":"out.md"}`}}
	case 1:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: `{"path":"out.md","content":"isolated document\n"}`}}
	case 2:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "verify", Name: "bash", Arguments: `{"command":"[ -f out.md ]"}`}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
