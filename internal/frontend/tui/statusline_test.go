package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

func statuslineModel(t *testing.T, line string) (*model, func() string) {
	t.Helper()
	t.Setenv("REASONIX_DISABLE_MOUSE", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"label": "flash", "used": 1200, "window": 64000, "cwd": "/work",
				"cacheHit": 90, "cacheMiss": 10,
			})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	var mu sync.Mutex
	var stdin string
	m := newModel(context.Background(), Options{
		Client: &Client{HTTP: srv.Client(), Base: srv.URL},
		Statusline: func(_ context.Context, in string) string {
			mu.Lock()
			stdin = in
			mu.Unlock()
			return line
		},
	})
	closed := make(chan Update)
	close(closed)
	m.updates = closed
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	run(m, m.fetchStatus())
	return m, func() string { mu.Lock(); defer mu.Unlock(); return stdin }
}

func TestStatuslineCommandReplacesTheTelemetryRowAfterATurn(t *testing.T) {
	m, stdin := statuslineModel(t, "custom-row main*")
	if got := strings.Join(m.statusBlock(), "\n"); !strings.Contains(got, i18n.M.ChatStatusCacheLabel) {
		t.Fatalf("before any turn the built-in telemetry shows:\n%s", got)
	}
	_, cmd := m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "turn_done"}}}, ok: true})
	run(m, cmd)

	got := strings.Join(m.statusBlock(), "\n")
	if !strings.Contains(got, "custom-row main*") {
		t.Fatalf("status block lacks the command's line:\n%s", got)
	}
	if strings.Contains(got, i18n.M.ChatStatusCacheLabel) {
		t.Fatalf("the command's line should replace the telemetry row:\n%s", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdin()), &payload); err != nil {
		t.Fatalf("stdin is not JSON: %q", stdin())
	}
	want := map[string]any{"model": "flash", "contextUsed": float64(1200), "contextWindow": float64(64000), "cwd": "/work"}
	for k, v := range want {
		if payload[k] != v {
			t.Fatalf("payload[%q] = %v, want %v (payload %v)", k, payload[k], v, payload)
		}
	}
}

func TestEmptyStatuslineOutputKeepsTheBuiltInRow(t *testing.T) {
	m, _ := statuslineModel(t, "")
	_, cmd := m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "turn_done"}}}, ok: true})
	run(m, cmd)
	if got := strings.Join(m.statusBlock(), "\n"); !strings.Contains(got, i18n.M.ChatStatusCacheLabel) {
		t.Fatalf("an empty line should leave the built-in telemetry:\n%s", got)
	}
}
