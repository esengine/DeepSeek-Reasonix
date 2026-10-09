package extension

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProviderStreamRejectsNegativeUsage(t *testing.T) {
	cases := []struct {
		name    string
		chunk   StreamChunk
		invalid bool
	}{
		{"prompt", UsageChunk(ProviderUsage{PromptTokens: -1}), true},
		{"completion", UsageChunk(ProviderUsage{CompletionTokens: -1}), true},
		{"total", UsageChunk(ProviderUsage{TotalTokens: -1}), true},
		{"cache hit", UsageChunk(ProviderUsage{CacheHitTokens: -1}), true},
		{"cache miss", UsageChunk(ProviderUsage{CacheMissTokens: -1}), true},
		{"reasoning", UsageChunk(ProviderUsage{ReasoningTokens: -1}), true},
		{"text with negative usage", StreamChunk{Type: ChunkText, Text: "bad", Usage: &ProviderUsage{PromptTokens: -1}}, true},
		{"nil usage", StreamChunk{Type: ChunkUsage}, true},
		{"zero usage", UsageChunk(ProviderUsage{}), false},
		{"positive usage", UsageChunk(ProviderUsage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12, CacheHitTokens: 2, CacheMissTokens: 3, ReasoningTokens: 4, FinishReason: "stop"}), false},
		{"text with usage", StreamChunk{Type: ChunkText, Text: "ok", Usage: &ProviderUsage{TotalTokens: 1}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk {
				chunks := make(chan StreamChunk, 4)
				chunks <- TextChunk("prefix")
				chunks <- tc.chunk
				chunks <- TextChunk("trailer")
				chunks <- DoneChunk()
				close(chunks)
				return chunks
			}}
			host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
			host.handshake(t)
			opened := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("usage"))
			if opened.Err != nil {
				t.Fatalf("stream open: %+v", opened.Err)
			}
			end := host.waitStreamEnd()
			chunks, ends := host.streamNotifications()
			if len(ends) != 1 || end.StreamID != "usage" || end.Interrupted || len(chunks) == 0 || chunks[0].Chunk.Text != "prefix" {
				t.Fatalf("chunks=%+v ends=%+v", chunks, ends)
			}
			if tc.invalid {
				if len(chunks) != 1 || end.LastSeq != 1 || end.Error != "the extension provider produced an invalid chunk" {
					t.Fatalf("invalid usage delivered: chunks=%+v end=%+v", chunks, end)
				}
			} else {
				if len(chunks) != 4 || end.LastSeq != 4 || end.Error != "" || !reflect.DeepEqual(chunks[1].Chunk, tc.chunk) || chunks[2].Chunk.Text != "trailer" || chunks[3].Chunk.Type != ChunkDone {
					t.Fatalf("valid usage changed: chunks=%+v end=%+v", chunks, end)
				}
			}
			for i, chunk := range chunks {
				if chunk.Seq != int64(i+1) || chunk.StreamID != "usage" {
					t.Fatalf("chunk %d=%+v", i, chunk)
				}
			}
			catalog := host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
			var result ProviderCatalogResult
			if catalog.Err != nil || json.Unmarshal(catalog.Result, &result) != nil || result.Providers == nil {
				t.Fatalf("same connection catalog: %+v", catalog)
			}
		})
	}
}
