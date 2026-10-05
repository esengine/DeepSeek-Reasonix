package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
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

func TestStreamIgnoresEndpointOverridesThatRepeatBaseURL(t *testing.T) {
	for _, key := range []string{"request_url", "chat_url"} {
		t.Run(key, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.RequestURI()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			base := srv.URL + "/api/v1"
			p, err := New(provider.Config{
				Name: "relay", APIKey: "key", BaseURL: base, Model: "m",
				Extra: map[string]any{key: base + "/"},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			stream, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for chunk := range stream {
				if chunk.Type == provider.ChunkError {
					t.Fatalf("stream error: %v", chunk.Err)
				}
			}
			if got != "/api/v1/chat/completions" {
				t.Fatalf("POST %q, want /api/v1/chat/completions", got)
			}
		})
	}
}
