package agent

import (
	"reasonix/internal/event"
	"reasonix/internal/i18n"
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
	display := *call
	display.SourcesStatus = provider.ServerSearchSourcesStatus(display)
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
	if provider.ServerSearchSourcesStatus(*call) == provider.SourcesNotProvided && !dispatched[call.ID+"\x00sources"] {
		dispatched[call.ID+"\x00sources"] = true
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: "search_sources_not_provided", Text: i18n.M.SearchSourcesNotProvided})
	}
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: call.ID, Name: "web_search", Args: args, Output: provider.ServerSearchDisplayOutput(display), ReadOnly: true, AttemptID: attemptID,
	}})
}
