package installsource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

func TestManifestFetchKeepsSafeDiagnosticFactsAndIdentity(t *testing.T) {
	const source = "https://fixture-user:fixture-password@example.test/SKILL.md?token=fixture-token#fixture-fragment"
	for _, tc := range []struct {
		name, facts string
		status      int
		body        string
		readError   bool
		requestErr  error
		sentinel    error
	}{
		{name: "overflow", status: http.StatusOK, body: strings.Repeat("x", defaultFetchLimit+1), sentinel: ErrSourceUnreadable, facts: "response exceeds 2097152-byte limit"},
		{name: "partial", status: http.StatusPartialContent, sentinel: ErrSourceUnreadable, facts: "HTTP 206"},
		{name: "not-found", status: http.StatusNotFound, sentinel: ErrSourceUnreadable, facts: "HTTP 404"},
		{name: "unauthorized", status: http.StatusUnauthorized, sentinel: ErrAuthRequired, facts: "HTTP 401"},
		{name: "forbidden", status: http.StatusForbidden, sentinel: ErrAuthRequired, facts: "HTTP 403"},
		{name: "read-error", status: http.StatusOK, readError: true, sentinel: ErrSourceUnreadable},
		{name: "transport-error", requestErr: errors.New("fixture-peer-prose " + source), sentinel: ErrSourceUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: manifestSizeRoundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.requestErr != nil {
					return nil, tc.requestErr
				}
				var reader io.Reader = strings.NewReader(tc.body)
				if tc.readError {
					reader = iotest.ErrReader(errors.New("fixture-peer-prose " + source))
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(reader), Request: r}, nil
			})}
			tl := &Tool{httpClient: client}
			body, err := tl.fetchText(context.Background(), source)
			if body != "" || !errors.Is(err, tc.sentinel) {
				t.Fatalf("body=%q err=%v; want empty body and source identity", body, err)
			}
			projected := secrets.DiagnosticError(err)
			if !errors.Is(projected, tc.sentinel) {
				t.Fatal("diagnostic projection lost the source identity")
			}
			if errors.Is(tc.sentinel, ErrSourceUnreadable) {
				var refusal tool.Refusal
				if !errors.As(projected, &refusal) || refusal.Code != "install.source_unreadable" {
					t.Fatalf("diagnostic projection lost the refusal: %v", projected)
				}
			}
			for _, display := range []string{err.Error(), projected.Error()} {
				for _, secret := range []string{"fixture-user", "fixture-password", "fixture-token", "fixture-fragment", "fixture-peer-prose", "example.test"} {
					if strings.Contains(display, secret) {
						t.Fatalf("diagnostic exposed %q: %s", secret, display)
					}
				}
				if tc.facts != "" && !strings.Contains(display, tc.facts) {
					t.Fatalf("diagnostic omitted host facts %q: %s", tc.facts, display)
				}
			}
			if tc.name == "overflow" {
				var facts *hostFactError
				if !errors.As(projected, &facts) {
					t.Fatal("overflow bypassed the existing host fact owner")
				}
			}
		})
	}
}
