package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// The Responses wire runs web_search server-side and keeps the result text
// there — a completed web_search_call names the query and the sources it read,
// and only the model sees the pages. The card carries what the wire hands over.

// searchCall is the part of a completed web_search_call a card can show.
type searchCall struct {
	ID     string `json:"id"`
	Action struct {
		Query   string `json:"query"`
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
	} `json:"action"`
}

// searchCallChunk turns a validated web_search_call into the already-finished
// server tool every frontend already draws for the Messages API's.
func searchCallChunk(raw json.RawMessage) (provider.Chunk, bool) {
	var item searchCall
	if json.Unmarshal(raw, &item) != nil {
		return provider.Chunk{}, false
	}
	arguments := "{}"
	if query := strings.TrimSpace(item.Action.Query); query != "" {
		if encoded, err := json.Marshal(map[string]string{"query": query}); err == nil {
			arguments = string(encoded)
		}
	}
	return provider.Chunk{
		Type:     provider.ChunkProviderTool,
		Text:     formatSearchSources(item),
		ToolCall: &provider.ToolCall{ID: item.ID, Name: "web_search", Arguments: arguments},
	}, true
}

func formatSearchSources(item searchCall) string {
	var b strings.Builder
	for _, source := range item.Action.Sources {
		if url := strings.TrimSpace(source.URL); url != "" {
			fmt.Fprintf(&b, "\n- <%s>", url)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}
