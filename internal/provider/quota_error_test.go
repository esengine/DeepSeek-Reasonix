package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservedQuotaResponsesNeverRetryOrBlameCredentials(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{401, `{"error":{"type":"CreditsError","message":"Insufficient balance. https://example.test/private-billing"}}`, "CreditsError"},
		{402, `{"error":{"code":"too_many_requests","message":"Call failed: Insufficient token quota.","type":"rate_limit_error"}}`, "too_many_requests"},
		{429, `{"error":{"code":"insufficient_quota"}}`, "insufficient_quota"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			for _, managed := range []bool{false, true} {
				ctx := context.Background()
				if managed {
					ctx = WithManagedRecovery(ctx)
				}
				_, err := SendWithRetry(ctx, srv.Client(), SendOptions{Provider: "fixture", KeyPresent: true, RetryAuth: true}, func(ctx context.Context) (*http.Request, error) {
					return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
				})
				var quota *QuotaError
				var auth *AuthError
				if !errors.As(err, &quota) || errors.As(err, &auth) || quota.Status != tc.status || quota.Code != tc.code {
					t.Fatalf("wrong classification: %v", err)
				}
				if strings.Contains(err.Error(), "private-billing") || strings.Contains(err.Error(), "invalid") {
					t.Fatal("leaked private URL or misdiagnosed credentials")
				}
				f := ClassifyRecovery(err)
				if f.Phase != "quota" || f.Retryable {
					t.Fatalf("recovery=%+v", f)
				}
			}
			if calls != 2 {
				t.Fatalf("attempts=%d want one per call", calls)
			}
		})
	}
	if AsQuotaError(&AuthError{Status: 401, Body: `{"error":{"type":"AuthError","message":"Missing API key."}}`}) != nil {
		t.Fatal("auth classified as quota")
	}
	if ClassifyRecovery(&APIError{Status: 503, Body: `{"error":{"message":"billing service temporarily unavailable"}}`}).Phase == "quota" {
		t.Fatal("temporary billing service error treated as exhausted quota")
	}
}
