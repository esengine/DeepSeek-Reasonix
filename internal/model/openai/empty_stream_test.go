package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

// A gateway can answer 200, hold the stream open long enough to look healthy,
// and end it having sent nothing. Read as an empty answer, that costs the turn
// three retries and ends "the model finished without a visible final answer" —
// which names the wrong party and suggests a fix that cannot work.
func TestAnEmptyStreamIsAnUpstreamFailureNotAnEmptyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush(w)
		// A heartbeat and a clean end, which is what a model that is unavailable
		// or still warming up looks like on the wire.
		_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		flush(w)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush(w)
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "openrouter", BaseURL: srv.URL, Model: "ox-alpha", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var failure error
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			failure = chunk.Err
		}
	}
	if failure == nil {
		t.Fatal("a stream that carried nothing ended silently; the turn above can only read that as an empty answer")
	}
	var interrupted *provider.StreamInterruptedError
	if !errors.As(failure, &interrupted) {
		t.Fatalf("failure = %v, want a StreamInterruptedError the request layer can retry", failure)
	}
	if reason := provider.StreamInterruptReason(failure); reason != provider.StreamInterruptUpstreamError {
		t.Fatalf("reason = %q, want %q — the far side is what failed", reason, provider.StreamInterruptUpstreamError)
	}
	if !errors.Is(failure, errEmptyStream) {
		t.Fatalf("failure = %v, want it to carry errEmptyStream", failure)
	}
}

// A stream that carried only a usage record did reach the model: it answered,
// and it answered with nothing. That is the case the turn's retry exists for,
// so it must not be turned into a transport failure.
func TestAUsageOnlyStreamIsStillTheModelAnswering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush(w)
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n")
		flush(w)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush(w)
	}))
	defer srv.Close()

	p, _ := New(provider.Config{Name: "openrouter", BaseURL: srv.URL, Model: "ox-alpha", APIKey: "k"})
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("a usage-bearing stream was reported as a transport failure: %v", chunk.Err)
		}
	}
}
