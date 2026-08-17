package responses

import (
	"context"
	"encoding/json"

	"reasonix/internal/provider"
)

func emitSearchReplay(ctx context.Context, out chan<- provider.Chunk, raw json.RawMessage) bool {
	if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkResponsesItem, ResponsesItem: raw}) {
		return false
	}
	if search := provider.ServerSearchFromResponsesItem(raw); search != nil {
		return sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: search})
	}
	return true
}
