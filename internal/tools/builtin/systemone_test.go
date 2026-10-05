package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/model/laya"
)

func TestSystemOneToolReturnsCompleteDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"route":{"type":"choice","choice":"billing","probabilities":{"billing":0.88,"support":0.12},"confidence":0.81}},"usage":{"input_tokens":318,"output_tokens":12}}`))
	}))
	defer server.Close()
	decision := NewSystemOne(SystemOneSpec{HTTP: server.Client(), BaseURL: server.URL, APIKey: func() string { return "secret" }})
	out := runTool(t, decision, map[string]any{
		"state": "payout failed",
		"questions": map[string]any{
			"route": map[string]any{"type": "choice", "instructions": "Choose a team", "criteria": map[string]any{"billing": nil, "support": nil}},
		},
	})
	for _, want := range []string{`"choice":"billing"`, `"billing":0.88`, `"confidence":0.81`, `"input_tokens":318`} {
		if !strings.Contains(out, want) {
			t.Fatalf("result %q does not contain %q", out, want)
		}
	}
}

// A self-hosted gateway was a second backend with its own client, its own
// config section and its own settings form. It answers the same wire at another
// address, so it is now reached as an ordinary source — by the name settings
// gave it, which is what the model is offered.
func TestSystemOneReachesASelfHostedEndpointByItsSourceName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"english","answers":{"risk":{"type":"noul","noul":0.7}},"routing":{"model":"english"}}`))
	}))
	defer server.Close()
	decision := NewSystemOne(SystemOneSpec{
		Name: "laya", HTTP: server.Client(), BaseURL: server.URL, Model: "auto",
		APIKey: func() string { return "gateway-secret" },
	})
	out := runTool(t, decision, map[string]any{
		"backend": "laya", "state": "cancel my plan",
		"questions": map[string]any{"risk": map[string]any{"type": "noul", "instructions": "Will the user churn?"}},
	})
	if !strings.Contains(out, `"noul":0.7`) || !strings.Contains(out, `"routing"`) {
		t.Fatalf("result = %s", out)
	}
}

// The model is offered the backends that exist, under the names settings shows.
func TestSystemOneOffersOnlyConfiguredBackends(t *testing.T) {
	decision := NewSystemOne(SystemOneSpec{Name: "typesafe", APIKey: func() string { return "secret" }})
	schema := string(decision.Schema())
	if !strings.Contains(schema, `"enum":["typesafe"]`) {
		t.Fatalf("schema offers more than is configured: %s", schema)
	}
}

func TestSystemOneRequiresBackendWhenSeveralAreConfigured(t *testing.T) {
	decision := NewSystemOne(SystemOneSpec{
		Name:      "typesafe",
		APIKey:    func() string { return "secret" },
		LayaLocal: &laya.LocalClient{Python: "python", Model: "auto"},
	})
	_, err := decision.Execute(context.Background(), argsJSON(t, map[string]any{
		"state": "x", "questions": map[string]any{"q": map[string]any{"type": "noul", "instructions": "yes?"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "backend is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestSystemOneToolVisibilityFollowsCredential(t *testing.T) {
	key := ""
	decision := NewSystemOne(SystemOneSpec{APIKey: func() string { return key }}).(interface {
		ProviderVisible(context.Context) bool
	})
	if decision.ProviderVisible(context.Background()) {
		t.Fatal("tool visible without a key")
	}
	key = "secret"
	if !decision.ProviderVisible(context.Background()) {
		t.Fatal("tool hidden with a key")
	}
}
