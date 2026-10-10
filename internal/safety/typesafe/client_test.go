package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluateUsesSystemOneWireContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request = %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "jev-latest" || len(request.Questions) != 3 {
			t.Fatalf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0.92}},"usage":{"input_tokens":12,"output_tokens":2}}`))
	}))
	defer server.Close()

	client := Client{HTTP: server.Client(), BaseURL: server.URL, APIKey: func() string { return "secret" }}
	response, err := client.Evaluate(context.Background(), Request{
		State: "payout failed",
		Model: "jev-latest",
		Questions: map[string]Question{
			"urgent": {Type: "noul", Instructions: "Is this urgent?"},
			"route":  {Type: "choice", Instructions: "Choose a team", Criteria: map[string]any{"billing": nil, "support": nil}},
			"risk":   {Type: "score", Instructions: "Rate risk", Criteria: []any{"low", "high"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "jev-1.13.0" || response.Usage.InputTokens != 12 || len(response.Answers) != 1 {
		t.Fatalf("response = %+v", response)
	}
}

func TestEvaluateKeepsHTTPFailureIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"bad question"}`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), BaseURL: server.URL, APIKey: func() string { return "secret" }}
	_, err := client.Evaluate(context.Background(), Request{State: "x", Model: "jev-latest", Questions: map[string]Question{"q": {Type: "noul", Instructions: "yes?"}}})
	var httpError *HTTPError
	if !errors.As(err, &httpError) || httpError.Status != http.StatusUnprocessableEntity {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluateValidatesQuestionBoundsBeforeNetwork(t *testing.T) {
	client := Client{APIKey: func() string { return "secret" }}
	_, err := client.Evaluate(context.Background(), Request{State: "x", Model: "jev-latest", Questions: map[string]Question{"q": {Type: "score", Instructions: "rate", Criteria: []any{"only"}}}})
	if err == nil {
		t.Fatal("expected invalid score criteria to fail")
	}
}

func probeClient(t *testing.T, reply http.HandlerFunc, key string) Client {
	t.Helper()
	server := httptest.NewServer(reply)
	t.Cleanup(server.Close)
	return Client{HTTP: server.Client(), BaseURL: server.URL, APIKey: func() string { return key }}
}

func TestProbeAcceptsAValidNoulVerdict(t *testing.T) {
	client := probeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"answers":{"probe":{"type":"noul","noul":1}}}`))
	}, "k")
	if err := client.Probe(context.Background(), "m"); err != nil {
		t.Fatal(err)
	}
}

func TestProbeFailuresCarryIdentity(t *testing.T) {
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
	}
	for name, body := range map[string]string{
		"not json":      `<html>`,
		"no answer":     `{"answers":{}}`,
		"wrong type":    `{"answers":{"probe":{"type":"choice","noul":0.5}}}`,
		"missing noul":  `{"answers":{"probe":{"type":"noul"}}}`,
		"out of range":  `{"answers":{"probe":{"type":"noul","noul":1.5}}}`,
		"negative noul": `{"answers":{"probe":{"type":"noul","noul":-0.1}}}`,
	} {
		if err := probeClient(t, ok(body), "k").Probe(context.Background(), "m"); !errors.Is(err, ErrMalformedResponse) {
			t.Errorf("%s: err = %v, want ErrMalformedResponse", name, err)
		}
	}
	if err := probeClient(t, ok(`{}`), "").Probe(context.Background(), "m"); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("no key: err = %v", err)
	}
	refused := probeClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }, "k")
	var httpErr *HTTPError
	if err := refused.Probe(context.Background(), "m"); !errors.As(err, &httpErr) || httpErr.Status != http.StatusTeapot {
		t.Errorf("status: err = %v", err)
	}
	if err := probeClient(t, ok(`{}`), "k").Probe(context.Background(), " "); err == nil {
		t.Error("an empty model must be refused before any request")
	}
}
