package boot

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/model/openai"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/stats"
)

// What a person reads on the usage page must be what the provider's server
// actually received, per provider, from a real assembled session.
func TestEffectModelRequestBytesReachTheUsageDirectory(t *testing.T) {
	home, dir := statedBootHome(t), testenv.TempDir(t)
	var received, requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Add(int64(len(body)))
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	adapter, err := openai.New(provider.Config{Name: "meter-wire", BaseURL: srv.URL, Model: "test", APIKey: "test", Extra: map[string]any{"protocol": "none"}})
	if err != nil {
		t.Fatal(err)
	}
	kind := "boot-upload-meter-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return adapter, nil })
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[[providers]]
name = "test-model"
kind = %q
model = "x"
`, kind))
	approveWorkspaceAt(t, home, dir)

	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard, StatsSource: surface.CLI})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	for _, prompt := range []string{"first question", "second question"} {
		if err := ctrl.Run(t.Context(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	stats.FlushUploadMeter()

	now := time.Now()
	got, err := stats.QueryUpload(config.RootsForHome(home).StatsDir(), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != requests.Load() || got.Requests < 2 {
		t.Fatalf("meter counted %d requests, server saw %d", got.Requests, requests.Load())
	}
	if got.Bytes != received.Load() {
		t.Fatalf("meter says %d bytes were sent, the server received %d", got.Bytes, received.Load())
	}
	if len(got.Providers) != 1 || got.Providers[0].Provider != "meter-wire" {
		t.Fatalf("providers = %+v, want every byte under the provider instance that sent it", got.Providers)
	}
}
