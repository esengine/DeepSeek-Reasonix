package anthropic

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// The Messages API can run web_search itself: the query arrives as a
// server_tool_use block, its results as web_search_tool_result. Neither is a call
// the client answers, so they leave as one already-finished call.

// serverBlock is the server-run call a result block answers. A result follows the
// block that issued it, so the latest one is the one being answered. owed holds
// result blocks that opened empty: some gateways send the array in a later
// web_search_tool_result_delta instead of the block start.
type serverBlock struct {
	call  provider.ToolCall
	index int
	owed  map[int]string // result block index -> tool_use_id
}

// blockStart is a content_block_start payload.
type blockStart struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"` // web_search_tool_result
	Content   json.RawMessage `json:"content"`     // web_search_tool_result: array of result objects
}

// begin records a server-run block; a result block that already carries its
// array becomes a call at once. It reports false once the consumer is gone.
func (s *serverBlock) begin(index int, b *blockStart, send func(provider.Chunk) bool) bool {
	switch b.Type {
	case "server_tool_use":
		s.call, s.index = provider.ToolCall{ID: b.ID, Name: b.Name}, index
	case "web_search_tool_result":
		if chunk, ok := webSearchChunk(s.call, b.ToolUseID, index, b.Content); ok {
			return send(chunk)
		}
		if s.owed == nil {
			s.owed = map[int]string{}
		}
		s.owed[index] = b.ToolUseID
	}
	return true
}

// args accumulates the query of the server-run call, reporting whether the
// fragment belonged to it.
func (s *serverBlock) args(index int, partial string) bool {
	if index != s.index {
		return false
	}
	s.call.Arguments += partial
	return true
}

// resultDelta completes an owed result block. It answers once per block, so a
// gateway repeating the array does not produce a second card.
func (s *serverBlock) resultDelta(index int, results json.RawMessage, send func(provider.Chunk) bool) bool {
	id, owed := s.owed[index]
	if !owed {
		return true
	}
	chunk, ok := webSearchChunk(s.call, id, index, results)
	if !ok {
		return true
	}
	delete(s.owed, index)
	return send(chunk)
}

// webSearchResult is a single result from a web_search_tool_result block.
type webSearchResult struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	SiteName string `json:"site_name"`
}

// webSearchChunk pairs a result block with the query that produced it. The index
// rides the ID because a turn can search several times and a gateway may omit
// tool_use_id — frontends merge cards by ID, and a shared one folds every search
// of the turn into a single card.
func webSearchChunk(issued provider.ToolCall, toolUseID string, index int, results json.RawMessage) (provider.Chunk, bool) {
	formatted := formatWebSearchResults(results)
	if formatted == "" {
		return provider.Chunk{}, false
	}
	return provider.Chunk{
		Type: provider.ChunkProviderTool,
		Text: formatted,
		ToolCall: &provider.ToolCall{
			ID:        fmt.Sprintf("%s#%d", toolUseID, index),
			Name:      cmp.Or(issued.Name, "web_search"),
			Arguments: issued.Arguments,
		},
	}, true
}

// formatWebSearchResults parses a web_search_tool_result content array
// and formats titles and URLs as human-readable text. DeepSeek returns
// encrypted_content rather than plain text at the transport layer; the
// model still sees the original content.
func formatWebSearchResults(raw json.RawMessage) string {
	var results []webSearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return ""
	}
	var b strings.Builder
	for _, r := range results {
		if r.Title == "" && r.URL == "" {
			continue
		}
		fmt.Fprintf(&b, "\n- **%s**", r.Title)
		if r.URL != "" {
			fmt.Fprintf(&b, "\n  <%s>", r.URL)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String() + "\n"
}
