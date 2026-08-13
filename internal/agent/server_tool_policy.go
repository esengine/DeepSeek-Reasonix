package agent

import (
	"errors"

	"reasonix/internal/provider"
)

var errServerToolDuringFinalization = errors.New("provider returned a server-side tool while server tools were disabled for finalization")

// enforceServerToolPolicy turns forbidden provider work into the stream's
// existing error path before it can be displayed or committed.
func enforceServerToolPolicy(req provider.Request, chunk provider.Chunk) provider.Chunk {
	if req.DisableServerTools && chunk.Type == provider.ChunkServerSearch {
		return provider.Chunk{Type: provider.ChunkError, Err: errServerToolDuringFinalization}
	}
	return chunk
}
