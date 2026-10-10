package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
)

const typedCheckKey = "sk-typed-check-0123456789"

// checkSavedProvider points the fixture provider at baseURL and runs the
// whole-connection check through the real assembled kernel and the HTTP handler,
// returning the raw JSON object a frontend receives.
func checkSavedProvider(t *testing.T, baseURL string) (map[string]any, string) {
	t.Helper()
	s := savedProviderServer(t, baseURL)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`)
	defer resp.Body.Close()
	raw, err := readAllString(resp)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /providers/check = %d: %s", resp.StatusCode, raw)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	return got, raw
}

func savedProviderServer(t *testing.T, baseURL string) *Server {
	t.Helper()
	s := newProviderEditServer(t)
	credentials := config.UserCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte("EXISTING_API_KEY="+typedCheckKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("existing")
	if !ok {
		t.Fatal("fixture provider is missing")
	}
	entry.BaseURL = baseURL
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	s.AllowProviderEdit()
	return s
}

func TestCheckProviderFailureCarriesTheCodeStatusAndEndpointWords(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		words  string
		code   string
	}{
		{"refused key", http.StatusUnauthorized, `{"error":{"message":"Invalid Authentication"}}`, "Invalid Authentication", "provider.probe.unauthorized"},
		{"forbidden key", http.StatusForbidden, `{"error":{"message":"region blocked"}}`, "region blocked", "provider.probe.unauthorized"},
		{"no balance", http.StatusPaymentRequired, `{"error":{"message":"Insufficient Balance"}}`, "Insufficient Balance", "provider.probe.payment_required"},
		{"rate limited", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, "slow down", "provider.probe.rate_limited"},
		{"path missing", http.StatusNotFound, `{"error":{"message":"no such route"}}`, "no such route", "provider.probe.path_not_found"},
		{"gateway down", http.StatusBadGateway, `upstream exploded`, "upstream exploded", "provider.probe.upstream_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer upstream.Close()

			got, raw := checkSavedProvider(t, upstream.URL+"/v1")
			if got["ok"] != false || got["code"] != tt.code {
				t.Fatalf("check = %s, want ok=false code=%s", raw, tt.code)
			}
			if got["httpStatus"] != float64(tt.status) {
				t.Fatalf("httpStatus = %v, want %d: %s", got["httpStatus"], tt.status, raw)
			}
			detail, _ := got["detail"].(string)
			if !strings.Contains(detail, tt.words) {
				t.Fatalf("detail = %q, want the endpoint's own words from %q", detail, tt.body)
			}
			if _, free := got["error"]; free {
				t.Fatalf("a failed check still carries free text as its only answer: %s", raw)
			}
		})
	}
}

func TestCheckProviderNoChatModelsCarriesTheCount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"text-embedding-3-large"},{"id":"bge-reranker-v2"}]}`)
	}))
	defer upstream.Close()

	got, raw := checkSavedProvider(t, upstream.URL+"/v1")
	if got["ok"] != false || got["code"] != "provider.probe.no_chat_models" {
		t.Fatalf("check = %s, want no_chat_models", raw)
	}
	params, _ := got["params"].(map[string]any)
	if params["count"] != float64(2) {
		t.Fatalf("params = %v, want count 2", got["params"])
	}
	if _, has := got["httpStatus"]; has {
		t.Fatalf("an answered listing has no failing status: %s", raw)
	}
}

func TestCheckProviderUnreachableHasItsOwnCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	got, raw := checkSavedProvider(t, "http://"+addr+"/v1")
	if got["ok"] != false || got["code"] != "provider.probe.unreachable" {
		t.Fatalf("check = %s, want unreachable", raw)
	}
	if _, has := got["httpStatus"]; has {
		t.Fatalf("a connection that never answered has no status: %s", raw)
	}
}

func TestCheckProviderTimeoutIsNotUnreachable(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)

	s := savedProviderServer(t, upstream.URL+"/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/providers/check", strings.NewReader(`{"name":"existing"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	operatorHandler(s).ServeHTTP(rec, req)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	if got["ok"] != false || got["code"] != "provider.probe.timeout" {
		t.Fatalf("check = %s, want timeout", rec.Body.String())
	}
}

func TestProbeFindingWithoutAProbeIdentityStillCarriesACode(t *testing.T) {
	got := probeFinding(errors.New("not a probe error"), func() string { return typedCheckKey })
	if got.OK || got.Code != codeProbeFailed {
		t.Fatalf("finding = %+v, want ok=false and %s", got, codeProbeFailed)
	}
	if !translatedCodes(t)[codeProbeFailed] {
		t.Fatalf("kernel.ts has no wording for %s", codeProbeFailed)
	}
}

func TestCheckProviderSuccessCarriesNoFailureFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+typedCheckKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"model-a"},{"id":"model-b"}]}`)
	}))
	defer upstream.Close()

	got, raw := checkSavedProvider(t, upstream.URL+"/v1")
	if got["ok"] != true {
		t.Fatalf("check = %s, want ok", raw)
	}
	for _, field := range []string{"code", "httpStatus", "detail", "params", "error"} {
		if _, has := got[field]; has {
			t.Fatalf("a passing check carries %q: %s", field, raw)
		}
	}
	if models, _ := got["models"].([]any); len(models) != 2 {
		t.Fatalf("models = %v, want both listed", got["models"])
	}
}

func TestCheckProviderNeverEchoesTheKey(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"bad key ` + typedCheckKey + `"}}`,
		"bad key\n" + typedCheckKey + "\x1b[31m and more",
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(body))
		}))
		got, raw := checkSavedProvider(t, upstream.URL+"/v1")
		upstream.Close()
		if strings.Contains(raw, typedCheckKey) {
			t.Fatalf("check response leaked the key: %s", raw)
		}
		if got["code"] != "provider.probe.unauthorized" {
			t.Fatalf("check = %s, want unauthorized", raw)
		}
	}
}

func TestCheckProviderDetailIsBoundedToOnePrintableLine(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("line one\nline two\t" + strings.Repeat("x ", 5000)))
	}))
	defer upstream.Close()

	got, raw := checkSavedProvider(t, upstream.URL+"/v1")
	detail, _ := got["detail"].(string)
	if strings.ContainsAny(detail, "\n\t") || len([]rune(detail)) > modelCheckDetailRunes+1 || !strings.HasPrefix(detail, "line one line two") {
		t.Fatalf("detail = %q (%d bytes of response), want one bounded line", detail, len(raw))
	}
}

func TestCheckProviderStillWaitsOnTheGrantAndKnowsUnknownNames(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`)
	defer resp.Body.Close()
	raw, _ := readAllString(resp)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(raw, "provider.editing_disabled") {
		t.Fatalf("ungranted check = %d %s, want 403 provider.editing_disabled", resp.StatusCode, raw)
	}
}

// The probe receives the key typed into the draft, and an endpoint's refusal can
// echo it; the refusal keeps the code and leaves the key behind.
func TestProbeRefusalNeverEchoesTheKey(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for _, body := range []string{
		`{"error":{"message":"bad key ` + typedCheckKey + `"}}`,
		"bad key\n" + typedCheckKey + "\x1b[31m and more",
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(body))
		}))
		draft, _ := json.Marshal(map[string]string{"baseUrl": upstream.URL + "/v1", "apiKey": typedCheckKey})
		resp := postProvider(t, srv.URL, "/providers/probe", string(draft))
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		upstream.Close()
		if strings.Contains(string(raw), typedCheckKey) {
			t.Fatalf("probe refusal leaked the key: %s", raw)
		}
		var got Reason
		if err := json.Unmarshal(raw, &got); err != nil || resp.StatusCode != http.StatusUnauthorized || got.Code != codeProbeUnauthorized {
			t.Fatalf("probe = %d %s, want 401 %s", resp.StatusCode, raw, codeProbeUnauthorized)
		}
	}
}

func TestProbeRefusalWithoutAProbeIdentityIsTyped(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProbeFailure(rec, errors.New("kernel fault "+typedCheckKey), typedCheckKey)
	var got Reason
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Code != codeProbeFailed {
		t.Fatalf("refusal = %d %s, want code %s", rec.Code, rec.Body.String(), codeProbeFailed)
	}
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), typedCheckKey) {
		t.Fatalf("refusal = %d %s, want 500 without the error's text", rec.Code, rec.Body.String())
	}
}
