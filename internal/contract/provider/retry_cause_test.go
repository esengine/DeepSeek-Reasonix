package provider

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout awaiting response headers" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func retryInfosFor(t *testing.T, opts SendOptions, first func() (*http.Response, error)) []RetryInfo {
	t.Helper()
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return first()
		}
		return statusResp(200, nil), nil
	})}
	var infos []RetryInfo
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = WithRetryNotify(ctx, func(i RetryInfo) {
		infos = append(infos, i)
		cancel()
	})
	_, _ = SendWithRetry(ctx, cl, opts, newDummyReq)
	return infos
}

func TestRetryInfoNamesTheFailureClass(t *testing.T) {
	cases := []struct {
		name       string
		first      func() (*http.Response, error)
		wantCause  RetryCause
		wantStatus int
	}{
		{"connection closed", func() (*http.Response, error) { return nil, io.ErrUnexpectedEOF }, RetryCauseConnectionClosed, 0},
		{"no answer in time", func() (*http.Response, error) { return nil, timeoutErr{} }, RetryCauseTimeout, 0},
		{"bad gateway", func() (*http.Response, error) { return statusResp(502, nil), nil }, RetryCauseUpstreamStatus, 502},
		{"rate limited", func() (*http.Response, error) { return statusResp(429, nil), nil }, RetryCauseUpstreamStatus, 429},
		{"other 5xx", func() (*http.Response, error) { return statusResp(503, nil), nil }, RetryCauseUpstreamStatus, 503},
		{"request timeout status", func() (*http.Response, error) { return statusResp(408, nil), nil }, RetryCauseUpstreamStatus, 408},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			infos := retryInfosFor(t, SendOptions{Provider: "p", HeaderTimeout: 90 * time.Second}, tc.first)
			if len(infos) != 1 {
				t.Fatalf("notifications = %d, want 1", len(infos))
			}
			got := infos[0]
			if got.Cause != tc.wantCause || got.Status != tc.wantStatus {
				t.Fatalf("cause=%q status=%d, want %q/%d", got.Cause, got.Status, tc.wantCause, tc.wantStatus)
			}
			if got.Timeout != 90*time.Second {
				t.Fatalf("timeout = %s, want the attempt's 90s header wait", got.Timeout)
			}
			if got.Delay <= 0 {
				t.Fatalf("delay = %s, want the backoff about to be slept", got.Delay)
			}
		})
	}
}

func TestRetryInfoTransientAuthIsAStatus(t *testing.T) {
	infos := retryInfosFor(t, SendOptions{Provider: "p", RetryAuth: true}, func() (*http.Response, error) { return statusResp(401, nil), nil })
	if infos[0].Cause != RetryCauseUpstreamStatus || infos[0].Status != 401 {
		t.Fatalf("got %q/%d, want upstream_status/401", infos[0].Cause, infos[0].Status)
	}
}

func TestRetryInfoReportsTheRetryAfterItWillSleep(t *testing.T) {
	resp := func() (*http.Response, error) {
		return statusResp(429, map[string]string{"Retry-After": "3"}), nil
	}
	infos := retryInfosFor(t, SendOptions{Provider: "p"}, resp)
	if infos[0].Delay != 3*time.Second {
		t.Fatalf("delay = %s, want the server's 3s Retry-After", infos[0].Delay)
	}
}

func TestStreamInterruptReasonMapsToRetryCause(t *testing.T) {
	cases := map[string]RetryCause{
		StreamInterruptConnectionReset: RetryCauseConnectionClosed,
		StreamInterruptPrematureEOF:    RetryCauseConnectionClosed,
		StreamInterruptIdleTimeout:     RetryCauseStreamIdle,
		StreamInterruptUpstreamError:   RetryCauseUpstreamError,
		"":                             "",
		"something_new":                "",
	}
	for reason, want := range cases {
		if got := RetryCauseOfStreamInterrupt(reason); got != want {
			t.Errorf("RetryCauseOfStreamInterrupt(%q) = %q, want %q", reason, got, want)
		}
	}
}
