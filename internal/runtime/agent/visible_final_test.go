package agent

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
)

import ()

func TestRunSubAgentStopsAfterRepeatedReasoningOnlyStops(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking 1"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 2"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 3"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
	}}

	_, err := RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, tool.NewRegistry(), sessionstore.NewSession("sys"),
		"analyze the code", Options{SubagentDepth: 1}, event.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "visible final answer") {
		t.Fatalf("error = %v, want bounded visible-final failure", err)
	}
	if prov.call != maxEmptyFinalBlocks {
		t.Fatalf("provider calls = %d, want %d", prov.call, maxEmptyFinalBlocks)
	}
}
