// Package event provides shared event handling utilities.
package event

import (
	"reasonix/internal/provider"
)

// ChunkEvent is a unified event type for both TUI and Desktop.
type ChunkEvent struct {
	Type      provider.ChunkType
	Text      string
	Reasoning string
	ToolCall  *provider.ToolCall
	Index     int
}

// Coalesce merges consecutive text/reasoning events from the same source.
// It preserves the order of non-text/reasoning events.
func Coalesce(events []ChunkEvent) []ChunkEvent {
	if len(events) <= 1 {
		return events
	}

	var out []ChunkEvent
	var currentText, currentReasoning string

	for _, e := range events {
		switch e.Type {
		case provider.ChunkText:
			// If we have pending reasoning, flush it first
			if currentReasoning != "" {
				out = append(out, ChunkEvent{
					Type:      provider.ChunkText,
					Text:      currentText,
					Reasoning: currentReasoning,
				})
				currentText = ""
				currentReasoning = ""
			}
			currentText += e.Text
		case provider.ChunkReasoning:
			// If we have pending text, flush it first
			if currentText != "" {
				out = append(out, ChunkEvent{
					Type:      provider.ChunkText,
					Text:      currentText,
					Reasoning: currentReasoning,
				})
				currentText = ""
				currentReasoning = ""
			}
			currentReasoning += e.Text
		default:
			// Flush pending text/reasoning
			if currentText != "" || currentReasoning != "" {
				out = append(out, ChunkEvent{
					Type:      provider.ChunkText,
					Text:      currentText,
					Reasoning: currentReasoning,
				})
				currentText = ""
				currentReasoning = ""
			}
			out = append(out, e)
		}
	}

	// Flush remaining text/reasoning
	if currentText != "" || currentReasoning != "" {
		out = append(out, ChunkEvent{
			Type:      provider.ChunkText,
			Text:      currentText,
			Reasoning: currentReasoning,
		})
	}

	return out
}

// canCoalesce checks if two events can be merged.
func canCoalesce(a, b provider.ChunkType) bool {
	return (a == provider.ChunkText && b == provider.ChunkText) ||
		(a == provider.ChunkReasoning && b == provider.ChunkReasoning)
}

// mergeEvent merges two consecutive text/reasoning events.
func mergeEvent(a, b ChunkEvent) ChunkEvent {
	if a.Type == provider.ChunkText && b.Type == provider.ChunkText {
		return ChunkEvent{
			Type:      provider.ChunkText,
			Text:      a.Text + b.Text,
			Reasoning: a.Reasoning,
		}
	}
	if a.Type == provider.ChunkReasoning && b.Type == provider.ChunkReasoning {
		return ChunkEvent{
			Type:      provider.ChunkReasoning,
			Text:      a.Text,
			Reasoning: a.Reasoning + b.Reasoning,
		}
	}
	return b
}