package agent

import (
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// thoughtClock measures one response's thinking: from its first reasoning text
// to the first answer output (text or a tool call). A response that answered
// without reasoning thought for no time; one that stopped mid-thought ends at
// its last reasoning text.
type thoughtClock struct{ start, last, end time.Time }

// observe reads one stream chunk: reasoning text keeps the thought going, and
// any answer output ends it.
func (c *thoughtClock) observe(chunk provider.Chunk, now time.Time) {
	switch chunk.Type {
	case provider.ChunkReasoning:
		if chunk.Text != "" {
			c.reasoning(now)
		}
	case provider.ChunkText, provider.ChunkToolCallStart, provider.ChunkToolCall:
		c.answer(now)
	}
}

// emitAssistantMessage sends the settled frame of one response, with the
// thinking time the transcript keeps for it; a response with neither text nor
// reasoning has no frame.
func emitAssistantMessage(sink event.Sink, text, reasoning string, thoughtMs int64) {
	if text == "" && reasoning == "" {
		return
	}
	sink.Emit(event.Event{Kind: event.Message, Text: DisplayAssistantText(text), Reasoning: reasoning, ThoughtMs: thoughtMs})
}

func (c *thoughtClock) reasoning(now time.Time) {
	if c.start.IsZero() {
		c.start = now
	}
	c.last = now
}

func (c *thoughtClock) answer(now time.Time) {
	if !c.start.IsZero() && c.end.IsZero() {
		c.end = now
	}
}

// ms is the measured thinking time in milliseconds, 0 when there was none.
func (c *thoughtClock) ms() int64 {
	if c.start.IsZero() {
		return 0
	}
	end := c.end
	if end.IsZero() {
		end = c.last
	}
	return end.Sub(c.start).Milliseconds()
}
