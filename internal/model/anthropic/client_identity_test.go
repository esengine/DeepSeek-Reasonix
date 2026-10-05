package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestMessagesIdentifyReasonix(t *testing.T) {
	provider.SetClientVersion("9.8.7")
	t.Cleanup(func() { provider.SetClientVersion("") })
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseFixture)
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "relay", BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got != "Reasonix/9.8.7" {
		t.Fatalf("User-Agent = %q, want Reasonix/9.8.7", got)
	}
}
