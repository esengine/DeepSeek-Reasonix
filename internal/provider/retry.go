package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxRetries is the number of times SendWithRetry re-attempts the connection +
// header phase after the initial try (so up to MaxRetries+1 total attempts).
const MaxRetries = 10

const maxBackoff = 15 * time.Second

// maxAuthRetries bounds how many times a 401/403 is retried for a key that has
// authenticated before: a transient server-side rejection (quota/gateway/rate)
// usually clears in a couple of attempts, whereas a key that never worked is a
// real config error and fails fast.
const maxAuthRetries = 2

// SendOptions carries the per-request context SendWithRetry needs to label
// errors and decide whether a 401 is worth retrying.
type SendOptions struct {
	Provider   string // provider instance name, surfaced in errors
	KeyEnv     string // api_key_env the key is read from, when known
	KeySource  string // human-readable source of KeyEnv, when known
	KeyPresent bool   // a non-empty key is being sent — separates "rejected" from "missing"
	RetryAuth  bool   // the key has authenticated before — retry transient 401s instead of failing fast
}

// RetryInfo describes a backoff about to happen: Attempt is the 1-based retry
// number (of Max) and Delay is how long SendWithRetry will wait before it.
type RetryInfo struct {
	Attempt int
	Max     int
	Delay   time.Duration
	Err     error
}

type RetryNotify func(RetryInfo)

type retryNotifyKey struct{}

// WithRetryNotify attaches a callback that SendWithRetry invokes before each
// backoff sleep, so the agent can surface a transient "retrying (n/m)" status.
func WithRetryNotify(ctx context.Context, fn RetryNotify) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, retryNotifyKey{}, fn)
}

func retryNotifyFromContext(ctx context.Context) RetryNotify {
	fn, _ := ctx.Value(retryNotifyKey{}).(RetryNotify)
	return fn
}

// APIError reports a non-OK HTTP status that isn't an auth failure. Status
// carries the code so the display layer can map it to an actionable, localized
// message; Body is a trimmed snippet of the response.
type APIError struct {
	Provider    string
	Status      int
	Body        string
	ToolContext string // resolved Reasonix/MCP identity for provider-indexed tool schema errors
}

func (e *APIError) Error() string {
	var base string
	if e.Body == "" {
		base = fmt.Sprintf("%s: status %d", e.Provider, e.Status)
	} else {
		base = fmt.Sprintf("%s: status %d: %s", e.Provider, e.Status, e.Body)
	}
	if e.ToolContext != "" {
		return base + "\n" + e.ToolContext
	}
	return base
}

// RetryableStatus reports whether a backoff can plausibly recover from status s:
// 408 (request timeout), 429 (rate limit) and 5xx (incl. Anthropic's 529). Other
// 4xx (400/401/402/422, …) are caller/config problems retrying can't fix.
func RetryableStatus(s int) bool {
	return s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || (s >= 500 && s <= 599)
}

func transientErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// IsConnReset reports whether err is a connection-level drop (peer reset,
// truncated body, closed socket) as opposed to a protocol or caller error. A
// stream cut this way mid-body can be replayed from scratch, unlike a decode or
// 4xx error. The common trigger is a local proxy (v2rayN/sing-box) idle-closing
// the long-lived SSE connection during a reasoner's first-token gap.
func IsConnReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxBackoff {
			return maxBackoff
		}
		return retryAfter
	}
	d := time.Duration(1<<(attempt-1)) * 500 * time.Millisecond
	if d > maxBackoff {
		d = maxBackoff
	}
	return d + time.Duration(rand.Intn(250))*time.Millisecond
}

func parseRetryAfter(resp *http.Response) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// SendWithRetry POSTs a streaming request built by newReq and returns the OK
// response. It retries the connection+header phase up to MaxRetries times on
// transient network errors and retryable statuses with capped exponential
// backoff + jitter, honoring Retry-After. A 401/403 becomes *AuthError: it
// fails fast for a key that has never authenticated (opts.RetryAuth false), but
// for a previously-good key it backs off and retries up to maxAuthRetries —
// MiMo and similar gateways return a transient 401 under load. Other non-OK
// statuses become *APIError. A RetryNotify in ctx fires before each sleep.
// Retries cover only the header phase — once the body streams, mid-stream
// failures are not retried (the model has already emitted tokens).
func SendWithRetry(ctx context.Context, httpClient *http.Client, opts SendOptions, newReq func(context.Context) (*http.Request, error)) (*http.Response, error) {
	notify := retryNotifyFromContext(ctx)
	var lastErr error
	var retryAfter time.Duration
	authRetries := 0

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt, retryAfter)
			if notify != nil {
				notify(RetryInfo{Attempt: attempt, Max: MaxRetries, Delay: delay, Err: lastErr})
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		retryAfter = 0

		req, err := newReq(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: build request: %w", opts.Provider, err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if !transientErr(err) {
				return nil, fmt.Errorf("%s: request failed: %w", opts.Provider, err)
			}
			lastErr = fmt.Errorf("%s: request failed: %w", opts.Provider, err)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		retryAfter = parseRetryAfter(resp)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			authErr := &AuthError{Provider: opts.Provider, KeyEnv: opts.KeyEnv, KeySource: opts.KeySource, Status: resp.StatusCode, HasKey: opts.KeyPresent, Body: strings.TrimSpace(string(msg))}
			if opts.RetryAuth && authRetries < maxAuthRetries {
				authRetries++
				lastErr = authErr
				continue
			}
			return nil, authErr
		}
		apiErr := &APIError{Provider: opts.Provider, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
		if !RetryableStatus(resp.StatusCode) {
			return nil, apiErr
		}
		lastErr = apiErr
	}
	return nil, lastErr
}
