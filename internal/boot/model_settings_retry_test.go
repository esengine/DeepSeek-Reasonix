package boot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

func TestModelSettingsHTTPRetryKeepsAcceptedCredential(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	var calls atomic.Int32
	keys := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		keys <- r.Header.Get("Authorization")
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	if _, err := config.SetCredential("RETRY_SNAPSHOT_KEY", "before-save"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DefaultModel = "p/m"
	cfg.Providers = []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: server.URL, Model: "m", APIKeyEnv: "RETRY_SNAPSHOT_KEY"}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	ctrl, err := Build(ctx, Options{WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	done := make(chan error, 1)
	go func() { done <- ctrl.RunTurn(ctx, "retry this request") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := config.SetCredential("RETRY_SNAPSHOT_KEY", "after-save"); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if calls.Load() != 2 {
		t.Fatalf("expected failed request and its successful retry, got %d", calls.Load())
	}
	for range 2 {
		if key := <-keys; key != "Bearer before-save" {
			t.Fatal("HTTP retry crossed credential generations")
		}
	}
	next, err := Build(ctx, Options{WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.RunTurn(ctx, "next request"); err != nil {
		t.Fatal(err)
	}
	if key := <-keys; key != "Bearer after-save" {
		t.Fatal("next runtime did not use the saved credential")
	}
}
