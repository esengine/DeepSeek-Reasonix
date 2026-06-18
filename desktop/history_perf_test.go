package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

type historyPerfCase struct {
	name       string
	turns      int
	outputSize int
	toolName   string
	failed     bool
}

func syntheticHistoryMessages(turns, outputSize int, toolName string, failed bool) []provider.Message {
	msgs := make([]provider.Message, 0, turns*4)
	output := strings.Repeat("x", outputSize)
	if failed {
		output = "error: " + output
	}
	for i := 0; i < turns; i++ {
		callID := fmt.Sprintf("call_%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("prompt %d", i)},
			provider.Message{
				Role:    provider.RoleAssistant,
				Content: fmt.Sprintf("answer %d", i),
				ToolCalls: []provider.ToolCall{{
					ID:        callID,
					Name:      toolName,
					Arguments: fmt.Sprintf(`{"command":"synthetic-%d","path":"file-%d.txt"}`, i, i),
				}},
			},
			provider.Message{
				Role:       provider.RoleTool,
				Name:       toolName,
				ToolCallID: callID,
				Content:    output,
			},
		)
	}
	return msgs
}

func historyPerfCases() []historyPerfCase {
	return []historyPerfCase{
		{name: "50x1MB-bash-success", turns: 50, outputSize: 1 << 20, toolName: "bash"},
		{name: "200x100KB-bash-success", turns: 200, outputSize: 100 << 10, toolName: "bash"},
		{name: "1000x10KB-bash-success", turns: 1000, outputSize: 10 << 10, toolName: "bash"},
		{name: "20x5MB-bash-success", turns: 20, outputSize: 5 << 20, toolName: "bash"},
		{name: "50x1MB-read-success", turns: 50, outputSize: 1 << 20, toolName: "read_file"},
		{name: "50x1MB-bash-error", turns: 50, outputSize: 1 << 20, toolName: "bash", failed: true},
	}
}

func BenchmarkHistoryMessagesSynthetic(b *testing.B) {
	for _, tc := range historyPerfCases() {
		b.Run(tc.name, func(b *testing.B) {
			msgs := syntheticHistoryMessages(tc.turns, tc.outputSize, tc.toolName, tc.failed)
			inputBytes := int64(tc.turns * tc.outputSize)
			b.ReportAllocs()
			b.SetBytes(inputBytes)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got := historyMessages(msgs, func(content string) string { return content })
				if len(got) == 0 {
					b.Fatal("empty history")
				}
			}
		})
	}
}

func BenchmarkHistoryMessagesMarshalSynthetic(b *testing.B) {
	for _, tc := range historyPerfCases() {
		b.Run(tc.name, func(b *testing.B) {
			msgs := syntheticHistoryMessages(tc.turns, tc.outputSize, tc.toolName, tc.failed)
			inputBytes := int64(tc.turns * tc.outputSize)
			b.ReportAllocs()
			b.SetBytes(inputBytes)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got := historyMessages(msgs, func(content string) string { return content })
				encoded, err := json.Marshal(got)
				if err != nil {
					b.Fatal(err)
				}
				if len(encoded) == 0 {
					b.Fatal("empty JSON")
				}
			}
		})
	}
}

func TestHistoryMessagesSyntheticPayloadSizes(t *testing.T) {
	msgs := syntheticHistoryMessages(10, 1<<20, "bash", false)
	got := historyMessages(msgs, func(content string) string { return content })
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic history payload bytes: %d", len(encoded))
}
