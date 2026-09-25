package boundedllm

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

type chunkProvider []provider.Chunk

func (chunkProvider) Name() string { return "chunks" }

func (c chunkProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(c))
	for _, chunk := range c {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func stop() provider.Chunk {
	return provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}
}

func TestCallReasoningAnswer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opt     bool
		chunks  chunkProvider
		want    string
		wantErr bool
	}{
		{"content wins over reasoning", true, chunkProvider{{Type: provider.ChunkReasoning, Text: "think"}, {Type: provider.ChunkText, Text: "answer"}, stop()}, "answer", false},
		{"reasoning-only stop answers when opted in", true, chunkProvider{{Type: provider.ChunkReasoning, Text: "ans"}, {Type: provider.ChunkReasoning, Text: "wer"}, stop()}, "answer", false},
		{"reasoning-only stop stays empty without opt-in", false, chunkProvider{{Type: provider.ChunkReasoning, Text: "answer"}, stop()}, "", false},
		{"reasoning without a stop is not an answer", true, chunkProvider{{Type: provider.ChunkReasoning, Text: "answer"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length"}}}, "", false},
		{"reasoning over the output budget fails", true, chunkProvider{{Type: provider.ChunkReasoning, Text: strings.Repeat("r", 65)}, stop()}, "", true},
		{"long reasoning before content is not charged", true, chunkProvider{{Type: provider.ChunkReasoning, Text: strings.Repeat("r", 65)}, {Type: provider.ChunkText, Text: "answer"}, stop()}, "answer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Call(context.Background(), Config{Provider: tc.chunks, MaxOutputBytes: 64, ReasoningAnswer: tc.opt}, "sys", "ev")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Call() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("Call() = %q, want %q", got, tc.want)
			}
		})
	}
}
