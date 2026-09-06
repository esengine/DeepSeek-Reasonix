package agent

import (
	"encoding/json"
	"reasonix/internal/provider"
	"strings"
)

type reasoningStreamMeta struct {
	signature, id, status string
	complete              bool
	state                 provider.ReasoningState
	blocks                []provider.ThinkingBlock
}

func (m *reasoningStreamMeta) ingest(chunk provider.Chunk, text *strings.Builder, limit int) {
	if m.state == "" {
		m.state = provider.ReasoningEmpty
	}
	if chunk.ReasoningState != "" && m.complete && (chunk.ReasoningState != provider.ReasoningEmpty || text.Len() == 0) {
		m.state = chunk.ReasoningState
	}
	text.WriteString(chunk.Text)
	if chunk.Signature != "" {
		m.signature = chunk.Signature
	}
	if chunk.ReasoningID != "" {
		m.id = chunk.ReasoningID
	}
	if chunk.ReasoningStatus != "" {
		m.status = chunk.ReasoningStatus
	}
	m.complete = boundReasoningReplay(text, chunk.Text, limit, m.complete)
	if chunk.ThinkingBlock != nil && m.complete {
		m.blocks = append(m.blocks, *chunk.ThinkingBlock)
	}
	bytes := 0
	for _, b := range m.blocks {
		bytes += len(b.Thinking) + len(b.Signature) + len(b.Data)
	}
	if limit > 0 && bytes > limit {
		m.complete = false
	}
	if !m.complete {
		m.state = provider.ReasoningTruncated
		m.blocks = nil
	} else if text.Len() > 0 && m.state == provider.ReasoningEmpty {
		m.state = provider.ReasoningComplete
	}
}

func (m *reasoningStreamMeta) ingestResponsesItem(items []json.RawMessage, raw json.RawMessage, limit int) []json.RawMessage {
	if len(raw) == 0 || !m.complete && provider.IsReplayableResponsesReasoning(raw) {
		return items
	}
	items = provider.UpsertResponsesItem(items, raw)
	if !withinReasoningItemsLimit(items, limit) {
		m.complete = false
		m.state = provider.ReasoningTruncated
		return nil
	}
	return items
}
