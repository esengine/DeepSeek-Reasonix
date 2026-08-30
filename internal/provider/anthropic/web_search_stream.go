package anthropic

import (
	"encoding/json"

	"reasonix/internal/provider"
)

type searchBlock struct {
	call provider.ServerSearchCall
	args string
}

type searchStream struct {
	byIndex map[int]*searchBlock
	byID    map[string]*searchBlock
}

func newSearchStream() *searchStream {
	return &searchStream{byIndex: map[int]*searchBlock{}, byID: map[string]*searchBlock{}}
}

func appendServerSearchBlocks(blocks []contentBlock, searches []provider.ServerSearchCall) []contentBlock {
	for _, search := range searches {
		if search.ID == "" {
			continue
		}
		input := json.RawMessage(`{}`)
		if search.Query != "" {
			if raw, err := json.Marshal(map[string]string{"query": search.Query}); err == nil {
				input = raw
			}
		}
		blocks = append(blocks, contentBlock{Type: "server_tool_use", ID: search.ID, Name: "web_search", Input: input})
		raw := search.Raw
		if len(raw) == 0 {
			raw = json.RawMessage("[]")
		}
		blocks = append(blocks, contentBlock{Type: "web_search_tool_result", ToolUseID: search.ID, Content: json.RawMessage(append(json.RawMessage(nil), raw...))})
	}
	return blocks
}

type streamContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

func beginContentBlock(index int, block *streamContentBlock, tools map[int]*provider.ToolCall, searches *searchStream) *provider.Chunk {
	if block == nil {
		return nil
	}
	switch block.Type {
	case "tool_use":
		tc := &provider.ToolCall{ID: block.ID, Name: block.Name}
		tools[index] = tc
		return &provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}
	case "server_tool_use":
		if started := searches.start(index, block.ID, block.Name); started != nil {
			return &provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: started}
		}
	case "web_search_tool_result":
		if result := searches.result(block.ToolUseID, block.Content); result != nil {
			// Register the result block's stream index too: streams that do not
			// inline the content deliver it via web_search_tool_result_delta.
			searches.byIndex[index] = searches.byID[block.ToolUseID]
			return &provider.Chunk{Type: provider.ChunkServerSearch, ServerSearch: result}
		}
	}
	return nil
}

func (s *searchStream) start(index int, id, name string) *provider.ServerSearchCall {
	if name != "web_search" || id == "" {
		return nil
	}
	block := &searchBlock{call: provider.ServerSearchCall{ID: id}}
	s.byIndex[index] = block
	s.byID[id] = block
	return &provider.ServerSearchCall{ID: id}
}

func (s *searchStream) result(id string, raw json.RawMessage) *provider.ServerSearchCall {
	block := s.byID[id]
	if block == nil {
		block = &searchBlock{call: provider.ServerSearchCall{ID: id}}
		if id != "" {
			s.byID[id] = block
		}
	}
	if len(raw) > 0 {
		block.call.Raw = append(json.RawMessage(nil), raw...)
		block.call.Results = provider.ParseServerSearchHits(raw)
	}
	return cloneServerSearch(block.call)
}

// resultsDelta ingests a web_search_tool_result_delta payload: the result
// array arrives after block start on streams that do not inline it in the
// web_search_tool_result content_block_start content.
func (s *searchStream) resultsDelta(index int, raw json.RawMessage) *provider.ServerSearchCall {
	block := s.byIndex[index]
	if block == nil || len(raw) == 0 {
		return nil
	}
	block.call.Raw = append(json.RawMessage(nil), raw...)
	block.call.Results = provider.ParseServerSearchHits(raw)
	return cloneServerSearch(block.call)
}

func (s *searchStream) argsDelta(index int, partial string) *provider.ServerSearchCall {
	block := s.byIndex[index]
	if block == nil || partial == "" {
		return nil
	}
	block.args += partial
	query := provider.ParseServerSearchQuery(block.args)
	if query == "" {
		return nil
	}
	block.call.Query = query
	return &provider.ServerSearchCall{ID: block.call.ID, Query: query}
}

func cloneServerSearch(call provider.ServerSearchCall) *provider.ServerSearchCall {
	out := call
	if len(call.Results) > 0 {
		out.Results = append([]provider.ServerSearchHit(nil), call.Results...)
	}
	if len(call.Raw) > 0 {
		out.Raw = append(json.RawMessage(nil), call.Raw...)
	}
	return &out
}

func formatWebSearchResults(raw json.RawMessage) string {
	return provider.FormatServerSearchFootnotes(provider.ParseServerSearchHits(raw))
}
