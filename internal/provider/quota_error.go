package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// QuotaError distinguishes exhausted credits/entitlement from invalid keys,
// including gateways which encode billing failures as HTTP 401 or 429. Raw
// bodies may contain private billing URLs or credentials and are not retained.
type QuotaError struct {
	Provider string
	Status   int
	Code     string
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("provider %q credits or subscription quota exhausted (HTTP %d); check account allowance before continuing", e.Provider, e.Status)
}

// Unwrap preserves status-based APIError consumers without exposing the raw
// billing response or making a quota rejection look like an AuthError.
func (e *QuotaError) Unwrap() error {
	return &APIError{Provider: e.Provider, Status: e.Status}
}

func QuotaErrorFromResponse(name string, status int, body string) *QuotaError {
	var v struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &v)
	code := v.Error.Code
	if code == "" {
		code = v.Error.Type
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{"insufficient_quota", "insufficient balance", "insufficient token quota", "out of budget", "quota exceeded", "freeusagelimiterror", "gousagelimiterror", "creditserror", "monthly usage limit reached", "available balance"} {
		if strings.Contains(lower, marker) {
			return &QuotaError{Provider: name, Status: status, Code: code}
		}
	}
	if status == 402 {
		return &QuotaError{Provider: name, Status: status, Code: code}
	}
	return nil
}

// AsQuotaError also handles legacy adapters returning structured API/Auth errors.
func AsQuotaError(err error) *QuotaError {
	var quota *QuotaError
	if errors.As(err, &quota) {
		return quota
	}
	var api *APIError
	if errors.As(err, &api) {
		return QuotaErrorFromResponse(api.Provider, api.Status, api.Body)
	}
	var auth *AuthError
	if errors.As(err, &auth) {
		return QuotaErrorFromResponse(auth.Provider, auth.Status, auth.Body)
	}
	return nil
}
