package providerext

import (
	"context"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"testing"
	"time"
)

func TestDefaultStreamIdleTimeoutIsFiveMinutes(t *testing.T) {
	if defaultStreamIdleTimeout != 300*time.Second {
		t.Fatalf("default stream idle timeout = %s, want 5m", defaultStreamIdleTimeout)
	}
}

// openTestStream resolves the demo ref and opens a stream, returning the
// chunk channel and the stream ID the sidecar would address.
func openTestStream(t *testing.T, r *Resolver, fc *fakeClient, effort *string) (<-chan provider.Chunk, string) {
	t.Helper()
	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x", Effort: effort})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out, err := p.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return out, fc.openedParams(t).StreamID
}

func textChunk(text string) protocol.ProviderChunk {
	return protocol.ProviderChunk{Type: protocol.ChunkText, Text: text}
}

// collectChunks drains the channel until it closes, failing on a wedge.
func collectChunks(t *testing.T, out <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var chunks []provider.Chunk
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-time.After(testBudget):
			t.Fatal("stream channel did not close")
		}
	}
}

func texts(chunks []provider.Chunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, c.Text)
	}
	return out
}
