package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func statusResp(status int, hdr map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range hdr {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("body")), Header: h}
}

func newDummyReq(ctx context.Context) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, "http://x/y", nil)
}

func TestRetryableStatus(t *testing.T) {
	for _, s := range []int{408, 429, 500, 502, 503, 504, 529, 599} {
		if !RetryableStatus(s) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	for _, s := range []int{200, 400, 401, 402, 403, 404, 422} {
		if RetryableStatus(s) {
			t.Errorf("status %d should not be retryable", s)
		}
	}
}

func TestTransientErr(t *testing.T) {
	if transientErr(nil) {
		t.Error("nil should not be transient")
	}
	if transientErr(context.Canceled) || transientErr(context.DeadlineExceeded) {
		t.Error("ctx cancel/deadline should not be transient")
	}
	if !transientErr(errors.New("connection reset")) {
		t.Error("network-ish error should be transient")
	}
}

func TestIsConnReset(t *testing.T) {
	if IsConnReset(nil) {
		t.Error("nil is not a conn reset")
	}
	if IsConnReset(context.Canceled) || IsConnReset(context.DeadlineExceeded) {
		t.Error("ctx cancel/deadline must not look like a recoverable reset")
	}
	if IsConnReset(errors.New("decode stream: invalid character")) {
		t.Error("a plain protocol error must not be treated as a conn reset")
	}
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Err: syscall.ECONNRESET},
		fmt.Errorf("read stream: %w", &net.OpError{Op: "read", Err: errors.New("wsarecv: forcibly closed")}),
	} {
		if !IsConnReset(err) {
			t.Errorf("want conn reset for %v", err)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	if d := backoffDelay(1, 0); d < 500*time.Millisecond || d >= 750*time.Millisecond {
		t.Errorf("attempt 1 base delay = %v, want [500ms,750ms)", d)
	}
	if d := backoffDelay(20, 0); d > maxBackoff+250*time.Millisecond {
		t.Errorf("delay %v exceeds cap+jitter", d)
	}
	if d := backoffDelay(5, 3*time.Second); d != 3*time.Second {
		t.Errorf("Retry-After should win: %v", d)
	}
	if d := backoffDelay(1, time.Hour); d != maxBackoff {
		t.Errorf("Retry-After should be capped to %v, got %v", maxBackoff, d)
	}
}

func TestSendWithRetryFailsFastOnClientErrors(t *testing.T) {
	for _, status := range []int{400, 402, 422} {
		calls := 0
		cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return statusResp(status, nil), nil
		})}
		_, err := SendWithRetry(context.Background(), cl, SendOptions{Provider: "p", KeyEnv: "KEY"}, newDummyReq)
		if calls != 1 {
			t.Errorf("status %d retried (%d calls), should fail fast", status, calls)
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != status {
			t.Errorf("status %d: want *APIError with Status=%d, got %v", status, status, err)
		}
	}
}

func TestSendWithRetryAuthError(t *testing.T) {
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return statusResp(401, nil), nil
	})}
	_, err := SendWithRetry(context.Background(), cl, SendOptions{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", KeyPresent: true}, newDummyReq)
	if calls != 1 {
		t.Errorf("401 retried (%d calls), should fail fast for a never-authed key", calls)
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.KeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("want *AuthError naming the key env, got %v", err)
	}
	if authErr != nil && authErr.Body != "body" {
		t.Errorf("AuthError should carry the response body, got %q", authErr.Body)
	}
}

func TestSendWithRetryRetriesTransientAuthForKnownKey(t *testing.T) {
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls <= 2 {
			return statusResp(401, nil), nil
		}
		return statusResp(200, nil), nil
	})}
	resp, err := SendWithRetry(context.Background(), cl,
		SendOptions{Provider: "mimo", KeyEnv: "MIMO_API_KEY", KeyPresent: true, RetryAuth: true}, newDummyReq)
	if err != nil {
		t.Fatalf("a previously-good key should recover from a transient 401: %v", err)
	}
	if resp.StatusCode != 200 || calls != 3 {
		t.Fatalf("status=%d calls=%d, want 200 after 3 calls", resp.StatusCode, calls)
	}
}

func TestSendWithRetryAuthGivesUpAfterMaxAuthRetries(t *testing.T) {
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return statusResp(401, nil), nil
	})}
	_, err := SendWithRetry(context.Background(), cl,
		SendOptions{Provider: "mimo", KeyEnv: "MIMO_API_KEY", KeyPresent: true, RetryAuth: true}, newDummyReq)
	if calls != 1+maxAuthRetries {
		t.Errorf("persistent 401 made %d calls, want %d (initial + maxAuthRetries)", calls, 1+maxAuthRetries)
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) || !authErr.HasKey {
		t.Fatalf("want *AuthError with HasKey=true, got %v", err)
	}
}

func TestSendWithRetryRecoversAndNotifies(t *testing.T) {
	calls := 0
	cl := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return statusResp(503, nil), nil
		}
		return statusResp(200, nil), nil
	})}
	var infos []RetryInfo
	ctx := WithRetryNotify(context.Background(), func(i RetryInfo) { infos = append(infos, i) })

	resp, err := SendWithRetry(ctx, cl, SendOptions{Provider: "p", KeyEnv: "KEY"}, newDummyReq)
	if err != nil {
		t.Fatalf("should recover after one retry: %v", err)
	}
	if resp.StatusCode != 200 || calls != 2 {
		t.Fatalf("status=%d calls=%d, want 200 after 2 calls", resp.StatusCode, calls)
	}
	if len(infos) != 1 || infos[0].Attempt != 1 || infos[0].Max != MaxRetries {
		t.Fatalf("retry notify = %#v, want one Attempt 1/%d", infos, MaxRetries)
	}
}
