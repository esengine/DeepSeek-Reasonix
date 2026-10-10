package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
)

type decisionService struct {
	mu    sync.Mutex
	paths []string
}

func (d *decisionService) record(r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paths = append(d.paths, r.Method+" "+r.URL.Path)
}

func (d *decisionService) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.paths...)
}

func decisionServer(t *testing.T, systemOne http.HandlerFunc) (*httptest.Server, *decisionService) {
	t.Helper()
	svc := &decisionService{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { svc.record(r); w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/systemone", func(w http.ResponseWriter, r *http.Request) { svc.record(r); systemOne(w, r) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		svc.record(r)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc
}

func checkSavedDecisionProvider(t *testing.T, baseURL string) map[string]any {
	t.Helper()
	t.Setenv("DECISION_API_KEY", "decision-key-0123456789")
	s := newProviderEditServer(t)
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("existing")
	if !ok {
		t.Fatal("fixture provider is missing")
	}
	entry.Kind = "typesafe"
	entry.BaseURL = baseURL
	entry.Models = []string{"jev-latest"}
	entry.Default = "jev-latest"
	entry.APIKeyEnv = "DECISION_API_KEY"
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	return decodeCheck(t, postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`))
}

func TestCheckProviderVerifiesADecisionSourceThroughItsOwnContract(t *testing.T) {
	srv, svc := decisionServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{"probe":{"type":"noul","noul":0.5}}}`))
	})
	got := checkSavedDecisionProvider(t, srv.URL)
	if got["ok"] != true || got["kind"] != "typesafe" || got["matches"] != true {
		t.Fatalf("check = %v; want a passing typesafe check", got)
	}
	for _, p := range svc.seen() {
		if p != "POST /v1/systemone" {
			t.Fatalf("decision source was asked for %q; only its own endpoint is part of the contract (%v)", p, svc.seen())
		}
	}
	if len(svc.seen()) == 0 {
		t.Fatal("the decision endpoint was never asked")
	}
}

func TestCheckProviderNamesWhyADecisionSourceFailed(t *testing.T) {
	tests := []struct {
		name   string
		reply  http.HandlerFunc
		code   string
		status float64
	}{
		{"path missing", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, "provider.probe.decision_path_not_found", 404},
		{"key refused", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }, "provider.probe.unauthorized", 401},
		{"not a decision answer", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>hi</html>`)) }, "provider.probe.decision_not_compatible", 0},
		{"answer without verdict", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"answers":{}}`)) }, "provider.probe.decision_not_compatible", 0},
		{"upstream broken", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "provider.probe.upstream_error", 502},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := decisionServer(t, tc.reply)
			got := checkSavedDecisionProvider(t, srv.URL)
			if got["ok"] == true || got["code"] != tc.code {
				t.Fatalf("check = %v; want code %s", got, tc.code)
			}
			if status, _ := got["httpStatus"].(float64); status != tc.status {
				t.Fatalf("httpStatus = %v, want %v", got["httpStatus"], tc.status)
			}
		})
	}
}

func TestCheckProviderDecisionSourceUnreachable(t *testing.T) {
	srv, _ := decisionServer(t, func(http.ResponseWriter, *http.Request) {})
	url := srv.URL
	srv.Close()
	got := checkSavedDecisionProvider(t, url)
	if got["ok"] == true || got["code"] != "provider.probe.unreachable" {
		t.Fatalf("check = %v; want provider.probe.unreachable", got)
	}
}

func decodeCheck(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCheckProviderDecisionStatusesKeepTheirOwnIdentity(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{http.StatusPaymentRequired, "provider.probe.payment_required"},
		{http.StatusTooManyRequests, "provider.probe.rate_limited"},
		{http.StatusGatewayTimeout, "provider.probe.timeout"},
		{http.StatusRequestTimeout, "provider.probe.timeout"},
		{http.StatusBadRequest, "provider.probe.decision_rejected"},
		{http.StatusUnprocessableEntity, "provider.probe.decision_rejected"},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv, _ := decisionServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"detail":"service words"}`))
			})
			got := checkSavedDecisionProvider(t, srv.URL)
			if got["code"] != tc.code || got["httpStatus"] != float64(tc.status) || got["detail"] != `{"detail":"service words"}` {
				t.Fatalf("check = %v; want %s with status and detail", got, tc.code)
			}
		})
	}
}

func TestCheckProviderDecisionWithoutAModelIsTypedAndAsksNothing(t *testing.T) {
	srv, svc := decisionServer(t, func(http.ResponseWriter, *http.Request) {})
	t.Setenv("DECISION_API_KEY", "decision-key-0123456789")
	s := newProviderEditServer(t)
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, _ := edit.Provider("existing")
	entry.Kind, entry.BaseURL, entry.Models, entry.Default, entry.APIKeyEnv = "typesafe", srv.URL, nil, "", "DECISION_API_KEY"
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	s.AllowProviderEdit()
	api := httptest.NewServer(operatorHandler(s))
	defer api.Close()
	got := decodeCheck(t, postProvider(t, api.URL, "/providers/check", `{"name":"existing"}`))
	if got["code"] != "provider.no_models_picked" || len(svc.seen()) != 0 {
		t.Fatalf("check = %v, requests = %v", got, svc.seen())
	}
}

func TestDecisionFindingReadsTransportIdentity(t *testing.T) {
	noKey := func() string { return "" }
	if got := decisionFinding(fmt.Errorf("call: %w", context.DeadlineExceeded), noKey); got.Code != "provider.probe.timeout" {
		t.Fatalf("deadline = %+v", got)
	}
	if got := decisionFinding(fmt.Errorf("call: %w", &net.DNSError{Err: "x", IsTimeout: true}), noKey); got.Code != "provider.probe.unreachable" {
		t.Fatalf("net error = %+v", got)
	}
	if got := decisionFinding(errors.New("opaque"), noKey); got.Code != "provider.probe.failed" {
		t.Fatalf("opaque = %+v", got)
	}
}

// The model check shares the probe with the connection check; these are the
// statuses and reasons the pre-extraction inline implementation produced for
// the same service replies, including a proxy setting netclient refuses.
func TestCheckProviderModelDecisionOutcomesAreUnchanged(t *testing.T) {
	tests := []struct {
		name        string
		reply       http.HandlerFunc
		status, why string
		httpStatus  float64
	}{
		{"ok", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"answers":{"probe":{"type":"noul","noul":0.5}}}`))
		}, "available", "", 0},
		{"unauthorized", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }, "unknown", "auth", 401},
		{"not found", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }, "unknown", "rejected", 404},
		{"rate limited", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, "unknown", "rate_limited", 429},
		{"upstream", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "unknown", "network", 502},
		{"html", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) }, "unknown", "rejected", 0},
		{"no verdict", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"answers":{}}`)) }, "unknown", "rejected", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := decisionServer(t, tc.reply)
			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			api := httptest.NewServer(operatorHandler(s))
			defer api.Close()
			body := fmt.Sprintf(`{"name":"x","model":"jev-latest","baseUrl":%q,"apiKey":"one-time-key","kind":"typesafe"}`, srv.URL)
			got := decodeCheck(t, postProvider(t, api.URL, "/providers/check/model", body))
			reason, _ := got["reason"].(string)
			status, _ := got["httpStatus"].(float64)
			if got["status"] != tc.status || reason != tc.why || status != tc.httpStatus {
				t.Fatalf("model check = %v", got)
			}
		})
	}
}

func TestCheckProviderModelDecisionWithARefusedProxyStaysRejected(t *testing.T) {
	srv, svc := decisionServer(t, func(http.ResponseWriter, *http.Request) {})
	s := newProviderEditServer(t)
	body := "[network]\nproxy_mode = \"custom\"\nproxy_url = \"::not a url\"\n"
	f, err := os.OpenFile(config.UserConfigPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n" + body); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s.AllowProviderEdit()
	api := httptest.NewServer(operatorHandler(s))
	defer api.Close()
	req := fmt.Sprintf(`{"name":"x","model":"jev-latest","baseUrl":%q,"apiKey":"one-time-key","kind":"typesafe"}`, srv.URL)
	got := decodeCheck(t, postProvider(t, api.URL, "/providers/check/model", req))
	if got["status"] != "unknown" || got["reason"] != "rejected" || len(svc.seen()) != 0 {
		t.Fatalf("model check = %v, requests = %v", got, svc.seen())
	}
}
