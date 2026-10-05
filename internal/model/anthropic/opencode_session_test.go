package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"reasonix/internal/contract/provider"
)

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

// The OpenCode Go Anthropic route refuses a request without a session (#10921).
func TestOpenCodeGoMessagesCarrySession(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-opencode-session")
		if got == "" {
			http.Error(w, `{"error":{"message":"Request is missing x-opencode-session"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseFixture)
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)

	p, err := New(provider.Config{Name: "opencode-go-anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "qwen3.6-plus", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.(*client).http = &http.Client{Transport: redirectTransport{target: target}}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got == "" {
		t.Fatal("request reached the gateway without x-opencode-session")
	}
}
