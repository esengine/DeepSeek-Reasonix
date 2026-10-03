package extension

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProviderStreamValidatesToolCallIdentifiers(t *testing.T) {
	cases := []struct {
		name    string
		chunk   StreamChunk
		invalid bool
	}{
		{"missing id", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{Name: "lookup"}}, true},
		{"blank id", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{ID: " \t\n", Name: "lookup"}}, true},
		{"missing name", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{ID: "call-1"}}, true},
		{"blank name", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{ID: "call-1", Name: " \t\n"}}, true},
		{"empty call", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{}}, true},
		{"text with invalid call", StreamChunk{Type: ChunkText, Text: "bad", ToolCall: &ProviderToolCall{ID: "call-1"}}, true},
		{"start with invalid call", StreamChunk{Type: ChunkToolCallStart, ToolCall: &ProviderToolCall{Name: "lookup"}}, true},
		{"delta with invalid call", StreamChunk{Type: ChunkToolCallDelta, ToolCall: &ProviderToolCall{ID: "call-1"}}, true},
		{"nil call", StreamChunk{Type: ChunkToolCall}, false},
		{"valid call", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{ID: "call-1", Name: "lookup", Arguments: `{"query":"reasonix"}`, ThoughtSignature: "sig"}}, false},
		{"unmodified identifiers", StreamChunk{Type: ChunkToolCall, ToolCall: &ProviderToolCall{ID: " 调用-1 ", Name: " 查询 ", Arguments: "partial"}}, false},
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
			opened := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("tool-call"))
			if opened.Err != nil {
				t.Fatalf("stream open: %+v", opened.Err)
			}
			end := host.waitStreamEnd()
			chunks, ends := host.streamNotifications()
			if len(ends) != 1 || end.StreamID != "tool-call" || end.Interrupted || len(chunks) == 0 || chunks[0].Chunk.Text != "prefix" {
				t.Fatalf("chunks=%+v ends=%+v", chunks, ends)
			}
			if tc.invalid {
				if len(chunks) != 1 || end.LastSeq != 1 || end.Error != "the extension provider produced an invalid chunk" {
					t.Fatalf("invalid tool call delivered: chunks=%+v end=%+v", chunks, end)
				}
			} else {
				if len(chunks) != 4 || end.LastSeq != 4 || end.Error != "" || !reflect.DeepEqual(chunks[1].Chunk, tc.chunk) || chunks[2].Chunk.Text != "trailer" || chunks[3].Chunk.Type != ChunkDone {
					t.Fatalf("valid tool call changed: chunks=%+v end=%+v", chunks, end)
				}
			}
			for i, chunk := range chunks {
				if chunk.Seq != int64(i+1) || chunk.StreamID != "tool-call" {
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
