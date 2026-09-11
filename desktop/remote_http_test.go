package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeClientRequiresLoopbackHTTP(t *testing.T) {
	for _, base := range []string{
		"https://127.0.0.1:1234",
		"http://example.com:1234",
		"http://user@127.0.0.1:1234",
	} {
		if _, err := newServeHTTPClient(base); err == nil {
			t.Fatalf("newServeHTTPClient(%q) succeeded", base)
		}
	}
	if _, err := newServeHTTPClient("http://127.0.0.1:1234"); err != nil {
		t.Fatalf("loopback client rejected: %v", err)
	}
}

func TestServeHandshakeDoesNotFollowRedirectWithToken(t *testing.T) {
	var leaked atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		leaked.Store(true)
	}))
	defer sink.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client, err := newServeHTTPClient(redirector.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := serveHandshake(ctx, client, redirector.URL, "secret-token"); err == nil {
		t.Fatal("redirecting handshake succeeded")
	}
	if leaked.Load() {
		t.Fatal("handshake followed the redirect and exposed its token body")
	}
}

func TestServeGetAcceptsHistoryLargerThanLegacyOneMiBLimit(t *testing.T) {
	payload := fmt.Sprintf(`{"history":"%s"}`, strings.Repeat("x", (2<<20)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	client, err := newServeHTTPClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := serveGet(ctx, client, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("history length = %d, want %d", len(got), len(payload))
	}
}
