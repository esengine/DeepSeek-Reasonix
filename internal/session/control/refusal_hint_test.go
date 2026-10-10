package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/anthropic"
	"reasonix/internal/model/openai"
)

// A request that left assistant reasoning out is a fact about the request, not a
// cause of the refusal: the endpoint's own words are the only evidence of why it
// said no, so the card carries them and asserts nothing the host cannot know.
func TestRefusalAfterDroppedReasoningShowsTheEndpointsWordsOnly(t *testing.T) {
	bodies := []string{
		"tools.0: Input tag 'function' found using 'type' does not match any of the expected tags: 'custom'",
		"Upstream request failed: Model is unavailable.",
		"The reasoning_content in the thinking mode must be passed back to the API.",
	}
	for _, body := range bodies {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			}))
			cfg := provider.Config{Name: "relay", BaseURL: srv.URL, Model: "deepseek-v3.2", APIKey: "test"}
			for _, build := range []func(provider.Config) (provider.Provider, error){openai.New, anthropic.New} {
				p, err := build(cfg)
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
					t.Fatal("a refusal must not be reported as a stream")
				}
				got := explainError(streamErr).Error()
				if !strings.Contains(got, body) {
					t.Errorf("status %d: refusal = %q, want the endpoint's words %q", status, got, body)
				}
				for _, claim := range []string{"thinking content", "思考内容", "reasoning protocol", "思考协议"} {
					if strings.Contains(got, claim) {
						t.Errorf("status %d, body %q: refusal asserts an unproven cause (%q): %q", status, body, claim, got)
					}
				}
			}
			srv.Close()
		}
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
