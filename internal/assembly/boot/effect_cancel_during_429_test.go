package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

func TestEffectStopDuringARateLimitedRetryEndsTheTurn(t *testing.T) {
	stopDuringProviderWait(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}, "each answer asks to wait 60s")
}

func TestEffectStopWhileTheProviderIsSilentEndsTheTurn(t *testing.T) {
	stopDuringProviderWait(t, nil, "the server never answers")
}

func stopDuringProviderWait(t *testing.T, handle func(http.ResponseWriter, *http.Request), why string) {
	t.Helper()
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	t.Setenv("RATE_LIMITED_FIXTURE_KEY", "sk-fixture")

	var hits atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if handle != nil {
			handle(w, r)
			return
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	writeFile(t, workspace, "reasonix.toml", `
default_model = "limited"
tool_approval = "yolo"

[codegraph]
enabled = false

[[providers]]
name = "limited"
kind = "openai"
base_url = "`+server.URL+`"
model = "m"
api_key_env = "RATE_LIMITED_FIXTURE_KEY"
`)
	approveWorkspace(t, workspace)

	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)

	done := make(chan error, 1)
	go func() { done <- ctrl.Run(context.Background(), "hello") }()

	deadline := time.Now().Add(20 * time.Second)
	for hits.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the provider was never asked")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	ctrl.Cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the turn was still running 10s after Stop (the server was asked %d times; %s)", hits.Load(), why)
	}
}
