package agent

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type searchTurn struct {
	calls      []provider.ServerSearchCall
	dispatched map[string]bool
}

func newSearchTurn() *searchTurn {
	return &searchTurn{dispatched: map[string]bool{}}
}

func (s *searchTurn) onChunk(sink event.Sink, chunk provider.Chunk, attemptID string) {
	if chunk.ServerSearch == nil {
		return
	}
	s.calls = provider.MergeServerSearch(s.calls, *chunk.ServerSearch)
	emitServerSearch(sink, chunk.ServerSearch, s.dispatched, attemptID)
}

func emitServerSearch(sink event.Sink, call *provider.ServerSearchCall, dispatched map[string]bool, attemptID string) {
	if call == nil {
		return
	}
	args := provider.FormatServerSearchArgs(call.Query)
	completed := len(call.Results) > 0 || len(call.Raw) > 0
	if !dispatched[call.ID] || !completed {
		sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			ID: call.ID, Name: "web_search", Args: args, ReadOnly: true, AttemptID: attemptID,
		}})
		dispatched[call.ID] = true
	}
	if !completed {
		return
	}
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: call.ID, Name: "web_search", Args: args, Output: provider.FormatServerSearchOutput(call.Results), ReadOnly: true, AttemptID: attemptID,
	}})
}
