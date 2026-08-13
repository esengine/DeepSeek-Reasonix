package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/provider"
)

func TestNewPrefersExactRequestURLOverLegacyChatURL(t *testing.T) {
	p, err := New(provider.Config{
		BaseURL: "https://base.example.com/v1",
		Model:   "model-a",
		Extra: map[string]any{
			"chat_url":    "https://legacy.example.com/chat/completions/",
			"request_url": "https://exact.example.com/custom/?token=1",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*client).chatURL; got != "https://exact.example.com/custom/?token=1" {
		t.Fatalf("chatURL = %q, want exact request_url", got)
	}
}

func TestNewCompletesOpenAIBaseRequestURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "root", in: "https://gateway.example.com", want: "https://gateway.example.com/chat/completions"},
		{name: "v1", in: "https://gateway.example.com/v1", want: "https://gateway.example.com/v1/chat/completions"},
		{name: "v1 trailing slash", in: "https://gateway.example.com/v1/", want: "https://gateway.example.com/v1/chat/completions"},
		{name: "root query", in: "https://gateway.example.com?trace=1", want: "https://gateway.example.com/chat/completions?trace=1"},
		{name: "v1 query", in: "https://gateway.example.com/v1/?trace=1", want: "https://gateway.example.com/v1/chat/completions?trace=1"},
		{name: "custom path", in: "https://gateway.example.com/custom/chat/?token=1", want: "https://gateway.example.com/custom/chat/?token=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(provider.Config{
				BaseURL: tc.in,
				Model:   "model-a",
				Extra:   map[string]any{"request_url": tc.in},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := p.(*client).chatURL; got != tc.want {
				t.Fatalf("chatURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStreamCompletesOpenAIBaseRequestURLBeforePosting(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		BaseURL: srv.URL + "/v1",
		Model:   "model-a",
		Extra:   map[string]any{"request_url": srv.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("posted path = %q, want /v1/chat/completions", gotPath)
	}
}
