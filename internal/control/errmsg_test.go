package control

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestExplainError(t *testing.T) {
	if explainError(nil) != nil {
		t.Error("nil should stay nil")
	}

	bal := explainError(&provider.APIError{Provider: "deepseek", Status: 402, Body: "Insufficient Balance"})
	if bal.Error() != i18n.M.ProviderErrInsufficientBalance {
		t.Errorf("402 = %q, want the insufficient-balance message", bal.Error())
	}

	auth := explainError(&provider.AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401})
	if !strings.Contains(auth.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("401 should name the key env: %q", auth.Error())
	}

	for _, status := range []int{400, 422, 429, 500, 503} {
		got := explainError(&provider.APIError{Provider: "p", Status: status})
		if got.Error() == "" || got.Error() == (&provider.APIError{Provider: "p", Status: status}).Error() {
			t.Errorf("status %d should map to a localized message, got %q", status, got.Error())
		}
	}

	jsonBody := explainError(&provider.APIError{Provider: "deepseek", Status: 400, Body: `{"error":{"message":"This model's maximum context length is 65536 tokens.","type":"invalid_request_error"}}`})
	if !strings.Contains(jsonBody.Error(), i18n.M.ProviderErrBadRequest) || !strings.Contains(jsonBody.Error(), "maximum context length") {
		t.Errorf("400 should append the provider reason from a JSON body, got %q", jsonBody.Error())
	}

	rawBody := explainError(&provider.APIError{Provider: "deepseek", Status: 422, Body: "some unparseable detail"})
	if !strings.Contains(rawBody.Error(), "some unparseable detail") {
		t.Errorf("422 should fall back to the raw body, got %q", rawBody.Error())
	}

	noLeak := explainError(&provider.APIError{Provider: "deepseek", Status: 429, Body: `{"error":{"message":"slow down"}}`})
	if noLeak.Error() != i18n.M.ProviderErrRateLimited {
		t.Errorf("429 body must not leak into the message, got %q", noLeak.Error())
	}

	plain := errors.New("some other failure")
	if explainError(plain) != plain {
		t.Error("unknown errors should pass through unchanged")
	}
}
