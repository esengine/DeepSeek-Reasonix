package event

import (
	"testing"

	"reasonix/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestCoalesce(t *testing.T) {
	tests := []struct {
		name     string
		input    []ChunkEvent
		expected []ChunkEvent
	}{
		{
			name:     "no events",
			input:    []ChunkEvent{},
			expected: []ChunkEvent{},
		},
		{
			name:     "single text event",
			input:    []ChunkEvent{{Type: provider.ChunkText, Text: "hello"}},
			expected: []ChunkEvent{{Type: provider.ChunkText, Text: "hello"}},
		},
		{
			name: "consecutive text events",
			input: []ChunkEvent{
				{Type: provider.ChunkText, Text: "hello"},
				{Type: provider.ChunkText, Text: " world"},
			},
			expected: []ChunkEvent{{Type: provider.ChunkText, Text: "hello world"}},
		},
		{
			name: "consecutive reasoning events",
			input: []ChunkEvent{
				{Type: provider.ChunkReasoning, Text: "<think>"},
				{Type: provider.ChunkReasoning, Text: "</think>"},
			},
			expected: []ChunkEvent{{Type: provider.ChunkText, Text: "", Reasoning: "<think></think>"}},
		},
		{
			name: "mixed text and reasoning",
			input: []ChunkEvent{
				{Type: provider.ChunkText, Text: "hello"},
				{Type: provider.ChunkReasoning, Text: "thinking"},
				{Type: provider.ChunkText, Text: " world"},
			},
			expected: []ChunkEvent{
				{Type: provider.ChunkText, Text: "hello", Reasoning: ""},
				{Type: provider.ChunkText, Text: "", Reasoning: "thinking"},
				{Type: provider.ChunkText, Text: " world", Reasoning: ""},
			},
		},
		{
			name: "non-text events preserved",
			input: []ChunkEvent{
				{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "1", Name: "test"}},
				{Type: provider.ChunkText, Text: "hello"},
				{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "test"}},
			},
			expected: []ChunkEvent{
				{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "1", Name: "test"}},
				{Type: provider.ChunkText, Text: "hello"},
				{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "test"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Coalesce(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}