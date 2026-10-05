package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

// A key is external mutable state: an exhausted one gets replaced while the
// session it started continues. Baking it into the client at construction meant
// the run kept presenting the dead key, and the balance error survived every
// attempt to fix it from the UI.
func TestStreamUsesTheKeyReplacedMidSession(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	live := "exhausted-key"
	p, err := New(provider.Config{
		Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4",
		APIKey:     "exhausted-key",
		APIKeyFunc: func() string { mu.Lock(); defer mu.Unlock(); return live },
	})
	if err != nil {
		t.Fatal(err)
	}
	drain := func() {
		ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for range ch {
		}
	}

	drain()
	mu.Lock()
	live = "funded-key" // what the user does in the provider panel
	mu.Unlock()
	drain()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(seen))
	}
	if seen[0] != "exhausted-key" {
		t.Errorf("first request key = %q", seen[0])
	}
	if seen[1] != "funded-key" {
		t.Errorf("second request key = %q, want the replacement; the session is still sending the dead key", seen[1])
	}
}

// An unreadable credential must not break a running session: the key the client
// was built with still answers.
func TestStreamFallsBackWhenTheLiveKeyIsUnavailable(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{
		Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4",
		APIKey:     "built-with",
		APIKeyFunc: func() string { return "  " },
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if seen != "built-with" {
		t.Errorf("key = %q, want the construction-time key when the live source is empty", seen)
	}
}

// staticKey is the fixed-key form for tests that build a client directly.
func staticKey(k string) func() string { return func() string { return k } }
