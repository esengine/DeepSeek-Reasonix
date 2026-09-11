package agent

import (
	"strings"

	"reasonix/internal/event"
)

// deferredStreamSink keeps selected stream events local until the caller
// chooses which provider response to adopt. On an ordinary healthy DeepSeek
// turn, reasoning arrives before tool calls and unlocks live tool-card events.
// On the rare malformed turn with no reasoning, only the speculative partial
// tool cards remain buffered, so retrying does not flash duplicate cards in the
// UI. A recovery attempt buffers everything because it may be discarded.
type deferredStreamSink struct {
	inner               event.Sink
	deferAll            bool
	waitingForReasoning bool
	sawReasoning        bool
	events              []event.Event
}

func newReasoningAwareStreamSink(inner event.Sink) *deferredStreamSink {
	return &deferredStreamSink{inner: inner, waitingForReasoning: true}
}

func newDeferredStreamSink(inner event.Sink) *deferredStreamSink {
	return &deferredStreamSink{inner: inner, deferAll: true}
}

func (s *deferredStreamSink) Emit(e event.Event) {
	if s == nil {
		return
	}
	if s.deferAll {
		s.events = append(s.events, e)
		return
	}
	if s.waitingForReasoning && e.Kind == event.Reasoning && strings.TrimSpace(e.Text) != "" {
		s.sawReasoning = true
		s.inner.Emit(e)
		s.flushBuffered()
		return
	}
	if s.waitingForReasoning && !s.sawReasoning {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult, event.Text, event.Message:
			// Keep every user-visible speculative event private until reasoning
			// proves the turn replayable. Healthy DeepSeek responses emit
			// reasoning first, so their live-streaming fast path is unchanged.
			s.events = append(s.events, e)
			return
		}
	}
	s.inner.Emit(e)
}

func (s *deferredStreamSink) flushBuffered() {
	if s == nil {
		return
	}
	for _, e := range s.events {
		s.inner.Emit(e)
	}
	s.events = nil
}

func (s *deferredStreamSink) Flush() {
	if s == nil {
		return
	}
	s.flushBuffered()
}

func (s *deferredStreamSink) Discard() {
	if s != nil {
		s.events = nil
	}
}
