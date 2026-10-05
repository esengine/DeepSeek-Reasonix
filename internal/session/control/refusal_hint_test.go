package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/openai"
)

// The whole point of the hint is that it survives: the only place that knows
// what the body left out is the client that built it, and by the time a person
// reads the failure the request is gone. This asserts the end of that path, not
// its middle — a real client, a real refusal, the text the turn ends with.
func TestRefusedBodyNamesTheProtocolInsteadOfBlamingItselfOnABug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"LITELLM_ERROR","message":"The reasoning_content in the thinking mode must be passed back to the API."}`))
	}))
	defer srv.Close()

	p, err := openai.New(provider.Config{
		Name:    "relay",
		BaseURL: srv.URL,
		Model:   "deepseek-v3.2",
		APIKey:  "test",
	})
	if err != nil {
		t.Fatalf("New relay: %v", err)
	}

	_, streamErr := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "count the go files"},
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "CHAIN-OF-THOUGHT",
			ToolCalls:        []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`}},
		},
		{Role: provider.RoleTool, Content: "14", ToolCallID: "c1", Name: "bash"},
	}})
	if streamErr == nil {
		t.Fatal("a 400 must not be reported as a stream")
	}

	got := explainError(streamErr).Error()
	if !strings.Contains(got, i18n.M.ProviderErrDroppedReasoning) {
		t.Errorf("refusal = %q, want the missing-protocol next step", got)
	}
	if strings.Contains(got, i18n.M.ProviderErrBadRequest) {
		t.Errorf("refusal still calls itself a bug to report: %q", got)
	}
}

// A refusal this host cannot explain keeps the generic status message. Nothing
// about the endpoint being unknown makes a guess about it useful.
func TestRefusalWithNoHintKeepsTheStatusMessage(t *testing.T) {
	got := explainError(&provider.APIError{Provider: "relay", Status: 400, Body: "context length exceeded"}).Error()
	if !strings.Contains(got, i18n.M.ProviderErrBadRequest) {
		t.Errorf("unhinted 400 = %q, want the generic bad-request message", got)
	}
}
