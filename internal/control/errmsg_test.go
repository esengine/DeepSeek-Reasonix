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

	plain := errors.New("some other failure")
	if explainError(plain) != plain {
		t.Error("unknown errors should pass through unchanged")
	}
}
