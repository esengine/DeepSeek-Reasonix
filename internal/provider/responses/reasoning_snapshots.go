package responses

import (
	"context"
	"encoding/json"
	"reasonix/internal/provider"
)

type responseReasoningSnapshots struct{ items []json.RawMessage }

func (s *responseReasoningSnapshots) capture(ev sseEvent) {
	var candidates []sseItem
	if ev.Type == "response.output_item.done" && ev.Item != nil {
		candidates = append(candidates, *ev.Item)
	}
	if ev.Type == "response.completed" && ev.Response != nil {
		candidates = append(candidates, ev.Response.Output...)
	}
	for _, item := range candidates {
		if provider.IsReplayableResponsesReasoning(item.Raw) {
			s.items = provider.UpsertResponsesItem(s.items, item.Raw)
		}
	}
}
func (s *responseReasoningSnapshots) emit(ctx context.Context, out chan<- provider.Chunk) bool {
	for _, item := range s.items {
		if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkResponsesItem, ResponsesItem: item}) {
			return false
		}
	}
	return true
}
func (s *responseReasoningSnapshots) metadata() (string, string) {
	var item sseItem
	_ = json.Unmarshal(s.items[len(s.items)-1], &item)
	return item.ID, item.Status
}
func appendReasoningItems(input []map[string]any, items []json.RawMessage) ([]map[string]any, bool) {
	found := false
	for _, raw := range items {
		if provider.IsReplayableResponsesReasoning(raw) {
			var item map[string]any
			if json.Unmarshal(raw, &item) == nil {
				input = append(input, item)
				found = true
			}
		}
	}
	return input, found
}

func terminalResponseID(event sseEvent) string {
	if event.Type == "response.completed" && event.Response != nil {
		return event.Response.ID
	}
	return ""
}
func emitTerminalResponseUsage(ctx context.Context, out chan<- provider.Chunk, event sseEvent) bool {
	if event.Response != nil {

		usage := usageFromResponse(event.Response)
		provider.ApplyRequestAttemptCount(ctx, usage)
		if event.Type == "response.incomplete" {
			switch event.Response.IncompleteDetails.Reason {
			case "max_output_tokens":
				usage.FinishReason = "length"
			case "content_filter":
				usage.FinishReason = "content_filter"
			default:
				usage.FinishReason = "incomplete"
			}
		} else if event.Type == "response.completed" && usage.FinishReason == "" {
			// A completed response finished normally (stop). Preserve any
			// vendor-specific reason already set by usageFromResponse.
			usage.FinishReason = "stop"
		}
		// Preserve a terminal finish reason even when the provider reports zero
		// usage, so reasoning-only completion is not mistaken for an empty reply.
		if usage.TotalTokens > 0 || usage.FinishReason != "" {

			if !sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkUsage, Usage: usage}) {
				return false
			}
		}
	}

	return true
}
