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
