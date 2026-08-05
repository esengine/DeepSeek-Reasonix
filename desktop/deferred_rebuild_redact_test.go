package main

import (
	"errors"
	"strings"
	"testing"
)

// TestDeferredReloadFailedTextRedactsCredentials is the regression for the
// CodeQL credential-disclosure finding: a deferred-reload failure may carry
// provider error text containing passwords or resolved API keys, and the
// user-visible notice must never repeat them.
func TestDeferredReloadFailedTextRedactsCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		secret string
	}{
		{"password field", errors.New(`provider auth failed: password=hunter2hunter2`), "hunter2hunter2"},
		{"api key assignment", errors.New(`401 unauthorized: api_key=sk-abcdef1234567890SECRETKEY`), "sk-abcdef1234567890SECRETKEY"},
		{"bearer token", errors.New(`upstream rejected: Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`), "eyJhbGciOiJIUzI1NiJ9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := deferredReloadFailedText(tc.err)
			if strings.Contains(text, tc.secret) {
				t.Fatalf("notice leaks the credential: %q", text)
			}
			if !strings.HasPrefix(text, "runtime reload failed: ") {
				t.Fatalf("notice lost its context prefix: %q", text)
			}
		})
	}
}
