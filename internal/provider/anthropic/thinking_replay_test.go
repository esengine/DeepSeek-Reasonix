package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reasonix/internal/provider"
	"strings"
	"testing"
)

func TestThinkingStreamPreservesInitialBlocksAndSplitSignatures(t *testing.T) {
	sse := `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"initial","signature":"prefix"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" tail"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"-a"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"-b"}}
data: {"type":"content_block_stop","index":0}
data: {"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"","signature":"empty-proof"}}
data: {"type":"content_block_stop","index":1}
data: {"type":"content_block_start","index":2,"content_block":{"type":"redacted_thinking","data":"redacted-proof"}}
data: {"type":"content_block_stop","index":2}
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}
data: {"type":"message_stop"}
`
	c := &client{name: "test", thinking: "adaptive"}
	ch := make(chan provider.Chunk)
	go c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(sse))}, ch)
	var text, signature string
	var blocks []provider.ThinkingBlock
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		text += chunk.Text
		if chunk.Signature != "" {
			signature = chunk.Signature
		}
		if chunk.ThinkingBlock != nil {
			blocks = append(blocks, *chunk.ThinkingBlock)
		}
	}
	if text != "initial tail" || len(blocks) != 3 || blocks[0].Signature != "prefix-a-b" || signature != "empty-proof" {
		t.Fatalf("text=%q signature=%q blocks=%+v", text, signature, blocks)
	}
	wire := c.replayReasoningBlocks(provider.Message{ThinkingBlocks: blocks})
	encoded, err := json.Marshal(wire)
	if err != nil || !strings.Contains(string(encoded), `"signature":"empty-proof","thinking":""`) || !strings.Contains(string(encoded), `"data":"redacted-proof"`) {
		t.Fatalf("replay=%s err=%v", encoded, err)
	}
}

func TestThinkingUnclosedBlockRemainsIncomplete(t *testing.T) {
	for _, terminal := range []string{"", "data: {\"type\":\"message_stop\"}\n"} {
		sse := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"partial\"}}\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n" + terminal
		c := &client{name: "test"}
		ch := make(chan provider.Chunk)
		go c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(sse))}, ch)
		var state provider.ReasoningState
		for chunk := range ch {
			if chunk.ReasoningState != "" {
				state = chunk.ReasoningState
			}
		}
		if state != provider.ReasoningIncomplete {
			t.Fatalf("state=%q", state)
		}
	}
}

func TestAdaptiveReplayPreservesHistoricalProofAcrossEffortChange(t *testing.T) {
	c := &client{thinking: "adaptive", effort: "disabled"}
	msg := provider.Message{Role: provider.RoleAssistant, ReasoningContent: "original", ReasoningSignature: "proof"}
	block, ok := c.replayReasoningBlock(msg)
	if !ok || block.Thinking != "original" || block.Signature != "proof" {
		t.Fatalf("historical proof changed: %+v", block)
	}
}
