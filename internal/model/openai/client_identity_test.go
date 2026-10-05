package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

func uaRecorder(t *testing.T, seen *[]string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Header.Get("User-Agent"))
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestsIdentifyReasonix(t *testing.T) {
	provider.SetClientVersion("9.8.7")
	t.Cleanup(func() { provider.SetClientVersion("") })
	var seen []string
	srv := uaRecorder(t, &seen, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")

	p, err := New(provider.Config{Name: "relay", BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	streamOnce(t, p)
	if _, err := FetchModelListing(context.Background(), srv.URL+"/v1", "k", FetchModelsOptions{}); err != nil {
		t.Fatalf("FetchModelListing: %v", err)
	}
	for i, ua := range seen {
		if ua != "Reasonix/9.8.7" {
			t.Fatalf("request %d User-Agent = %q, want Reasonix/9.8.7", i, ua)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("saw %d requests, want 2", len(seen))
	}
}

func TestConfiguredUserAgentWins(t *testing.T) {
	provider.SetClientVersion("9.8.7")
	t.Cleanup(func() { provider.SetClientVersion("") })
	var seen []string
	srv := uaRecorder(t, &seen, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")

	p, err := New(provider.Config{Name: "relay", BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k",
		Extra: map[string]any{"headers": map[string]string{"User-Agent": "custom/1"}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	streamOnce(t, p)
	if len(seen) != 1 || seen[0] != "custom/1" {
		t.Fatalf("User-Agent = %q, want the configured custom/1", seen)
	}
}
