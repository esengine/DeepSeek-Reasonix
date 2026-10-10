package control

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestContinuationRecoveryErrorPresentation(t *testing.T) {
	err := &provider.ContinuationRecoveryError{Err: &provider.APIError{Provider: "relay", Status: 400, Body: `{"error":{"message":"invalid input"}}`}}
	got := explainError(err).Error()
	if !strings.Contains(got, "full history without previous_response_id") || !strings.Contains(got, "invalid input") {
		t.Fatalf("recovery context or final provider reason missing: %s", got)
	}
}
