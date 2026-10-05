package provider

import "reasonix/internal/base/secrets"

// RedactMessage returns a storage-safe copy of m with textual secret surfaces
// masked; images are opaque data URLs and stay untouched. ToolCalls and
// MemoryCitations are cloned first: the save path hands in live session
// messages, and masking through shared slices would rewrite the model-visible
// history mid-conversation.
func RedactMessage(m Message) Message {
	m.Content = secrets.Redact(m.Content)
	m.ReasoningContent = secrets.Redact(m.ReasoningContent)
	m.Original = secrets.Redact(m.Original)
	if len(m.ToolCalls) > 0 {
		calls := make([]ToolCall, len(m.ToolCalls))
		copy(calls, m.ToolCalls)
		for i := range calls {
			calls[i].Arguments = secrets.Redact(calls[i].Arguments)
			calls[i].Diff = secrets.Redact(calls[i].Diff)
		}
		m.ToolCalls = calls
	}
	if len(m.MemoryCitations) > 0 {
		cites := make([]MemoryCitation, len(m.MemoryCitations))
		copy(cites, m.MemoryCitations)
		for i := range cites {
			cites[i].Note = secrets.Redact(cites[i].Note)
		}
		m.MemoryCitations = cites
	}
	return m
}

// RedactMessages returns a redacted copy of msgs. The input slice and its
// messages are never mutated.
func RedactMessages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = RedactMessage(m)
	}
	return out
}
