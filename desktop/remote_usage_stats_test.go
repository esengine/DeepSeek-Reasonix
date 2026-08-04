package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/stats"
)

type statsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f statsRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRemoteProviderStreamPersistsUsageInDesktopState(t *testing.T) {
	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	in := make(chan provider.Chunk, 2)
	in <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14, RequestCount: 2,
	}}
	in <- provider.Chunk{Type: provider.ChunkDone}
	close(in)

	for range recordRemoteProviderStream(context.Background(), "anthropic/claude", in) {
	}
	if err := stats.Flush(context.Background(), config.StatsDir()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	result, err := stats.NewWriter(config.StatsDir()).Query(stats.SourceFilter{
		From: now.AddDate(0, 0, -1), To: now.AddDate(0, 0, 1), Source: "remote",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tokens != 14 || result.Requests != 2 || result.Turns != 0 {
		t.Fatalf("remote totals = tokens %d requests %d turns %d, want 14/2/0", result.Tokens, result.Requests, result.Turns)
	}
	if len(result.Models) != 1 || result.Models[0].Model != "anthropic/claude" {
		t.Fatalf("remote models = %+v, want anthropic/claude", result.Models)
	}
	if len(result.Providers) != 1 || result.Providers[0].Provider != "anthropic" {
		t.Fatalf("remote providers = %+v, want anthropic", result.Providers)
	}
}

func TestRemoteProviderStreamPersistsRequestOnlyFailure(t *testing.T) {
	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	ctx := provider.WithRequestAttemptCounter(context.Background())
	client := &http.Client{Transport: statsRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("bad request")),
		}, nil
	})}
	_, requestErr := provider.SendWithRetry(ctx, client, provider.SendOptions{Provider: "remote"}, func(reqCtx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(reqCtx, http.MethodPost, "https://example.invalid", nil)
	})
	if requestErr == nil {
		t.Fatal("expected provider request failure")
	}
	in := make(chan provider.Chunk, 1)
	in <- provider.Chunk{Type: provider.ChunkError, Err: requestErr}
	close(in)
	for range recordRemoteProviderStream(ctx, "anthropic/claude", in) {
	}
	if err := stats.Flush(context.Background(), config.StatsDir()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	result, err := stats.NewWriter(config.StatsDir()).Query(stats.SourceFilter{
		From: now.AddDate(0, 0, -1), To: now.AddDate(0, 0, 1), Source: "remote",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tokens != 0 || result.Requests != 1 {
		t.Fatalf("remote failed totals = tokens %d requests %d, want 0/1", result.Tokens, result.Requests)
	}
}

func TestWorkbenchTurnDonePersistsRemoteCompletionLocally(t *testing.T) {
	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	app := &App{}
	k := app.workbench()
	_, generation, err := k.targets.BeginRemoteConnect("remote-host", "/srv/work")
	if err != nil {
		t.Fatal(err)
	}
	k.mu.Lock()
	k.remoteGen = generation
	k.mu.Unlock()

	app.workbenchClientCallbacks(generation, "").OnSessionEvent(protocol.SessionEvent{
		Seq: 1, Event: eventwire.Event{Kind: "turn_done"},
	})
	if err := stats.Flush(context.Background(), config.StatsDir()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	result, err := stats.NewWriter(config.StatsDir()).Query(stats.SourceFilter{
		From: now.AddDate(0, 0, -1), To: now.AddDate(0, 0, 1), Source: "remote",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Turns != 1 {
		t.Fatalf("remote turns = %d, want 1", result.Turns)
	}
}
