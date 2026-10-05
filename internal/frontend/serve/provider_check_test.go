package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// The endpoint in the fixture does not exist, so the probe fails — and that
// failure is the answer the row shows, carried as a finding rather than as a
// transport error the client has to guess at.
func TestCheckProviderReportsWhatTheEndpointSaid(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/check = %d: %s", resp.StatusCode, b)
	}
	var got providerCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.OK {
		t.Fatalf("an unreachable endpoint reported ok: %+v", got)
	}
	if got.Error == "" {
		t.Fatal("a failed probe must carry the endpoint's own words")
	}
	if len(got.Models) != 0 {
		t.Fatalf("a failed probe reported models: %v", got.Models)
	}
}

func TestCheckProviderRefusesAnUnknownName(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"nobody"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("checking an unknown provider = %d, want 404", resp.StatusCode)
	}
}

// A check spends the machine's stored credential against a network endpoint, so
// it waits on the same grant every other provider edit does.
func TestCheckProviderWaitsOnTheGrant(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /providers/check without the grant = %d, want 403", resp.StatusCode)
	}
}

func TestCheckProviderModelAcceptsAnUnlistedExactIDWithoutSavingIt(t *testing.T) {
	var wireModel string
	var wireMaxTokens int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		wireModel, wireMaxTokens = body.Model, body.MaxTokens
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"O\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("existing")
	if !ok {
		t.Fatal("fixture provider is missing")
	}
	entry.BaseURL = upstream.URL + "/v1"
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/providers/check/model", `{"name":"existing","model":"Hidden-Preview/Exact-ID"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/check/model = %d: %s", resp.StatusCode, b)
	}
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "available" || got.Reason != "" || got.Model != "Hidden-Preview/Exact-ID" {
		t.Fatalf("check = %+v, want the exact model reported available", got)
	}
	if wireModel != "Hidden-Preview/Exact-ID" || wireMaxTokens != 1 {
		t.Fatalf("wire model/max_tokens = %q/%d, want exact ID and one token", wireModel, wireMaxTokens)
	}
	saved := config.LoadForEdit(config.UserConfigPath())
	savedEntry, _ := saved.Provider("existing")
	if savedEntry.HasModel("Hidden-Preview/Exact-ID") {
		t.Fatal("an availability check added the model to persisted config")
	}
}

func TestCheckProviderModelUsesUnsavedEndpointAndKeyOnlyForTheProbe(t *testing.T) {
	authed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed = r.Header.Get("Authorization") == "Bearer one-time-key"
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"O\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body := fmt.Sprintf(`{"name":"not-saved","model":"private-model","baseUrl":%q,"apiKey":"one-time-key","kind":"openai"}`, upstream.URL+"/v1")
	resp := postProvider(t, srv.URL, "/providers/check/model", body)
	defer resp.Body.Close()
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "available" || !authed {
		t.Fatalf("check = %+v, authenticated = %v", got, authed)
	}
	if _, ok := config.LoadForEdit(config.UserConfigPath()).Provider("not-saved"); ok {
		t.Fatal("an inline probe persisted its temporary provider")
	}
}

func TestCheckProviderModelUsesSystemOneForTypeSafe(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer one-time-key" {
			t.Errorf("request = %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			State     any    `json:"state"`
			Model     string `json:"model"`
			Questions map[string]struct {
				Type         string `json:"type"`
				Instructions any    `json:"instructions"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.State == nil || request.Model != "jev-latest" || len(request.Questions) != 1 {
			t.Errorf("decision request = %+v", request)
		}
		for _, question := range request.Questions {
			if question.Type != "noul" || question.Instructions == nil {
				t.Errorf("question = %+v", question)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"probe":{"type":"noul","noul":0.9}}}`))
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body := fmt.Sprintf(`{"name":"not-saved","model":"jev-latest","baseUrl":%q,"apiKey":"one-time-key","kind":"typesafe"}`, upstream.URL)
	resp := postProvider(t, srv.URL, "/providers/check/model", body)
	defer resp.Body.Close()
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "available" || got.Model != "jev-latest" || calls != 1 {
		t.Fatalf("check = %+v, upstream calls = %d; want one successful System One request", got, calls)
	}
}

func TestCheckProviderModelUsesSavedDecisionKindAndEnvironmentKey(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer process-key" {
			t.Errorf("request path/auth = %q/%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"answers":{"probe":{"type":"noul","noul":0.5}}}`))
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	t.Setenv("TYPESAFE_API_KEY", "process-key")
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("existing")
	if !ok {
		t.Fatal("fixture provider is missing")
	}
	entry.Kind = "TypeSafe"
	entry.BaseURL = upstream.URL
	entry.APIKeyEnv = "TYPESAFE_API_KEY"
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	resp := postProvider(t, srv.URL, "/providers/check/model", `{"name":"existing","model":"jev-latest"}`)
	defer resp.Body.Close()
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "available" || calls != 1 {
		t.Fatalf("check = %+v, upstream calls = %d; want env-authenticated decision request", got, calls)
	}
}

func TestCheckProviderModelRejectsMalformedTypeSafeSuccess(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"answers":{"probe":{"type":"choice","noul":0.5}}}`,
		`{"answers":{"probe":{"type":"noul"}}}`,
		`{"answers":{"probe":{"type":"noul","noul":1.1}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()
			request := fmt.Sprintf(`{"name":"not-saved","model":"jev-latest","baseUrl":%q,"apiKey":"one-time-key","kind":"typesafe"}`, upstream.URL)
			resp := postProvider(t, srv.URL, "/providers/check/model", request)
			defer resp.Body.Close()
			var got providerModelCheck
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Status != "unknown" || got.Reason != "rejected" {
				t.Fatalf("check = %+v for response %s; want unconfirmed rejection", got, body)
			}
		})
	}
}

func TestCheckProviderModelClassifiesTypeSafeHTTPFailure(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		body       string
		status     string
		reason     string
	}{
		{"auth", http.StatusUnauthorized, `{"detail":"one-time-key is invalid"}`, "unknown", "auth"},
		{"rate limit", http.StatusTooManyRequests, `{"detail":"one-time-key is limited"}`, "unknown", "rate_limited"},
		{"missing model", http.StatusNotFound, `{"error":{"code":"model_not_found"}}`, "unavailable", "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/systemone" {
					t.Errorf("path = %q", r.URL.Path)
				}
				w.WriteHeader(tt.httpStatus)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer upstream.Close()

			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()
			body := fmt.Sprintf(`{"name":"not-saved","model":"jev-latest","baseUrl":%q,"apiKey":"one-time-key","kind":"typesafe"}`, upstream.URL)
			resp := postProvider(t, srv.URL, "/providers/check/model", body)
			defer resp.Body.Close()
			raw, err := readAllString(resp)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, "one-time-key") {
				t.Fatalf("response leaked the temporary key: %s", raw)
			}
			var got providerModelCheck
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status || got.Reason != tt.reason || got.HTTPStatus != tt.httpStatus || calls != 1 {
				t.Fatalf("check = %+v, upstream calls = %d; want %s/%s/%d in one request", got, calls, tt.status, tt.reason, tt.httpStatus)
			}
		})
	}
}

// A gateway that answers chat and refuses a tools array is the case a check
// without tools calls available, leaving the failure to the first real turn.
// The finding is established by the second attempt succeeding, never by
// reading the refusal's words.
func TestCheckProviderModelReportsAnEndpointThatRefusesTools(t *testing.T) {
	var withTools, withoutTools int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"tools"`) {
			withTools++
			http.Error(w, `{"error":{"type":"invalid_request_error","message":"unsupported"}}`, http.StatusBadRequest)
			return
		}
		withoutTools++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"O\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body := fmt.Sprintf(`{"name":"not-saved","model":"m","baseUrl":%q,"apiKey":"k","kind":"openai"}`, upstream.URL+"/v1")
	resp := postProvider(t, srv.URL, "/providers/check/model", body)
	defer resp.Body.Close()
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "unavailable" || got.Reason != "tools" {
		t.Fatalf("check = %+v, want the tools refusal named", got)
	}
	// Two downgrades meet and each asks once: the provider drops stream_options,
	// this check drops the tools array.
	if withTools != 2 || withoutTools != 1 {
		t.Fatalf("attempts with tools = %d, without = %d; want each downgrade asked once", withTools, withoutTools)
	}
}

// An endpoint that refuses everything must not be reported as a tools problem,
// and the retry must not turn one rejection into two findings.
func TestCheckProviderModelKeepsRejectionWhenRemovingToolsDoesNotHelp(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, `{"error":{"type":"invalid_request_error","param":"model"}}`, http.StatusUnprocessableEntity)
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body := fmt.Sprintf(`{"name":"not-saved","model":"m","baseUrl":%q,"apiKey":"k","kind":"openai"}`, upstream.URL+"/v1")
	resp := postProvider(t, srv.URL, "/providers/check/model", body)
	defer resp.Body.Close()
	var got providerModelCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "unavailable" || got.Reason != "rejected" {
		t.Fatalf("check = %+v, want the model rejection kept", got)
	}
	// Bounded rather than doubling: the provider asks about stream_options once
	// per endpoint however often a body is refused, so a gateway that rejects
	// everything sees the probe, that one question, and the retry without tools.
	if calls != 3 {
		t.Fatalf("upstream calls = %d, want the probe and one question from each downgrade", calls)
	}
}

func TestCheckProviderModelRateLimitIsOneRedactedAttempt(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, `{"error":{"message":"one-time-key is limited"}}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body := fmt.Sprintf(`{"name":"not-saved","model":"private-model","baseUrl":%q,"apiKey":"one-time-key"}`, upstream.URL+"/v1")
	resp := postProvider(t, srv.URL, "/providers/check/model", body)
	defer resp.Body.Close()
	raw, err := readAllString(resp)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want one explicit diagnostic attempt", calls)
	}
	if strings.Contains(raw, "one-time-key") {
		t.Fatalf("provider response leaked the one-time credential: %s", raw)
	}
	var got providerModelCheck
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "unknown" || got.Reason != "rate_limited" || got.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("check = %+v, want a structured rate-limit finding", got)
	}
}

func TestClassifyProviderModelCheckUsesTypedAndStructuredFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		status     string
		reason     string
		httpStatus int
	}{
		{"auth", &provider.AuthError{Status: http.StatusUnauthorized}, "unknown", "auth", http.StatusUnauthorized},
		{"rate", &provider.APIError{Status: http.StatusTooManyRequests}, "unknown", "rate_limited", http.StatusTooManyRequests},
		{"not found", &provider.APIError{Status: http.StatusNotFound, Body: `{"error":{"code":"model_not_found"}}`}, "unavailable", "not_found", http.StatusNotFound},
		{"model rejected", &provider.APIError{Status: http.StatusUnprocessableEntity, Body: `{"error":{"type":"invalid_request_error","param":"model"}}`}, "unavailable", "rejected", http.StatusUnprocessableEntity},
		{"generic rejection", &provider.APIError{Status: http.StatusBadRequest, Body: `{"error":{"message":"opaque"}}`}, "unknown", "rejected", http.StatusBadRequest},
		{"stream rate", &provider.StreamPayloadError{Code: "rate_limit_exceeded"}, "unknown", "rate_limited", 0},
		{"timeout", context.DeadlineExceeded, "unknown", "timeout", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason, httpStatus := classifyProviderModelCheck(tt.err)
			if status != tt.status || reason != tt.reason || httpStatus != tt.httpStatus {
				t.Fatalf("classify = %q/%q/%d, want %q/%q/%d", status, reason, httpStatus, tt.status, tt.reason, tt.httpStatus)
			}
		})
	}
}

// relayWithOnlyV1Models answers a model list under /v1 and nothing at the bare
// host, the shape of a relay whose chat lives under /v1 as well.
func relayWithOnlyV1Models(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-x"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeReturnsTheBaseThatAnswered(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	relay := relayWithOnlyV1Models(t)

	resp := postProvider(t, srv.URL, "/providers/probe", fmt.Sprintf(`{"baseUrl":%q,"apiKey":"k"}`, relay.URL))
	defer resp.Body.Close()
	var got struct {
		Kind    string `json:"kind"`
		BaseURL string `json:"baseUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "openai" || got.BaseURL != relay.URL+"/v1" {
		t.Fatalf("probe = %+v, want openai with baseUrl %s/v1", got, relay.URL)
	}

	save := postProvider(t, srv.URL, "/providers", fmt.Sprintf(`{"name":"relay","kind":"openai","baseUrl":%q,"apiKey":"k","models":["model-x"],"default":"model-x"}`, got.BaseURL))
	defer save.Body.Close()
	if save.StatusCode != http.StatusOK {
		b, _ := readAllString(save)
		t.Fatalf("save = %d: %s", save.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	saved, ok := cfg.Provider("relay")
	if !ok || saved.BaseURL != relay.URL+"/v1" {
		t.Fatalf("saved base = %+v, want the completed %s/v1", saved, relay.URL)
	}
}

func TestCheckReportsTheCompletedBaseWithoutWritingIt(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	relay := relayWithOnlyV1Models(t)
	t.Setenv("EXISTING_API_KEY", "k")
	if _, err := config.SetCredential("EXISTING_API_KEY", "k"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "https://example.invalid/v1", relay.URL)), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := postProvider(t, srv.URL, "/providers/check", `{"name":"existing"}`)
	defer resp.Body.Close()
	var got providerCheck
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.BaseURL != relay.URL+"/v1" {
		t.Fatalf("check = %+v, want ok with baseUrl %s/v1", got, relay.URL)
	}
	after, _ := os.ReadFile(path)
	if string(after) != strings.ReplaceAll(string(raw), "https://example.invalid/v1", relay.URL) {
		t.Fatal("a check rewrote the saved config")
	}
}
