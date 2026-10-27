package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/provider"
)

// A gateway that refuses stream_options is a fact about the endpoint, so the
// question is put once for the process, not once for every session's client.
func TestAnEndpointsRefusalOfStreamOptionsIsLearnedAcrossClients(t *testing.T) {
	var refused atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream_options"`) {
			refused.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unrecognized field stream_options"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}
	for session := 1; session <= 3; session++ {
		p, err := New(provider.Config{Name: "relay", BaseURL: srv.URL, Model: "m", APIKey: "k"})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := p.Stream(context.Background(), req)
		if err != nil {
			t.Fatalf("session %d: %v", session, err)
		}
		for range stream {
		}
	}
	if got := refused.Load(); got != 1 {
		t.Errorf("requests refused over stream_options across three clients = %d, want the one that established it", got)
	}
}
