package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MaxAssetSize caps a release artifact. A response that declares more is
// refused before it is read, so a redirect to something enormous cannot fill
// the disk while it is being verified.
const MaxAssetSize = int64(1 << 30)

// Attempts caps how many times a transient transport failure (connection reset,
// read timeout, gateway 5xx) is retried. CN IPv6 routes to Cloudflare reset
// mid-transfer often enough that a retry or two usually completes the download.
const Attempts = 3

// ErrTooLarge is a response bigger than MaxAssetSize. It is deliberately not
// transient: retrying cannot make it smaller.
var ErrTooLarge = errors.New("update: response exceeds allowed size")

// Backoff is the pause before the Nth retry; a variable so tests shrink it.
var Backoff = func(attempt int) time.Duration { return time.Duration(attempt) * 500 * time.Millisecond }

// StatusError is a response the server refused with a status. Typing it is what
// lets a 404 or a Cloudflare 403 be told apart from a network blip — an untyped
// error read as transient and burned every retry before surfacing.
type StatusError struct {
	URL    string
	Status string
	Code   int
}

func (e *StatusError) Error() string { return fmt.Sprintf("GET %s: %s", e.URL, e.Status) }

// Transient reports whether retrying err could plausibly succeed.
func Transient(err error) bool {
	if errors.Is(err, ErrTooLarge) || errors.Is(err, ErrStore) || errors.Is(err, ErrSizeMismatch) {
		return false
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		return true
	}
	return statusErr.Code == http.StatusRequestTimeout ||
		statusErr.Code == http.StatusTooManyRequests ||
		statusErr.Code >= 500
}

// Transport fetches release artifacts. Fallback is tried from the second
// attempt: a mid-transfer reset usually means the default (often IPv6) route is
// the problem, so the retry takes a different one.
type Transport struct {
	Client    *http.Client
	Fallback  *http.Client
	UserAgent string
	// AttemptTimeout bounds one Fetch attempt: a primary that stalls without
	// erroring would otherwise hold the budget and the retry that reaches the
	// fallback route would never run. Download has none; artifacts are large.
	AttemptTimeout time.Duration
	// StallTimeout drops a download connection that has delivered nothing for
	// this long, so the download resumes on a new one. Zero never drops it.
	StallTimeout time.Duration
}

// Retry runs attempt 1..Attempts of fetch until one succeeds, pausing between
// tries. It stops early on a non-transient failure or a cancelled context.
func Retry(ctx context.Context, fetch func(attempt int) error) error {
	var err error
	for attempt := 1; attempt <= Attempts; attempt++ {
		if err = fetch(attempt); err == nil {
			return nil
		}
		if !Transient(err) || ctx.Err() != nil || attempt == Attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(Backoff(attempt)):
		}
	}
	return err
}

// FetchFrom is Fetch over each address an object is published at, in order:
// the next address gets its own retry budget once one is spent or refused.
func (t Transport) FetchFrom(ctx context.Context, urls []string, maxBytes int64) ([]byte, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("update: no address to fetch from")
	}
	var errs []error
	for _, url := range urls {
		data, err := t.Fetch(ctx, url, maxBytes)
		if err == nil {
			return data, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.Join(errs...)
}

// Fetch GETs a URL fully into memory, bounded by maxBytes.
func (t Transport) Fetch(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("update: invalid response size limit %d", maxBytes)
	}
	var out []byte
	err := Retry(ctx, func(attempt int) error {
		attemptCtx, cancel := t.attemptContext(ctx)
		defer cancel()
		data, err := t.fetchOnce(attemptCtx, t.clientFor(attempt), url, maxBytes)
		if err != nil {
			return err
		}
		out = data
		return nil
	})
	return out, err
}

// FetchOnce is Fetch without the retry, for a caller that owns its own attempt
// budget — the manifest path splits one timeout across two transports and must
// not have a retry loop spend it.
func (t Transport) FetchOnce(ctx context.Context, c *http.Client, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("update: invalid response size limit %d", maxBytes)
	}
	if c == nil {
		c = t.clientFor(1)
	}
	return t.fetchOnce(ctx, c, url, maxBytes)
}

func (t Transport) attemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if t.AttemptTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, t.AttemptTimeout)
}

func (t Transport) clientFor(attempt int) *http.Client {
	if attempt > 1 && t.Fallback != nil {
		return t.Fallback
	}
	if t.Client == nil {
		return http.DefaultClient
	}
	return t.Client
}

func (t Transport) request(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if t.UserAgent != "" {
		req.Header.Set("User-Agent", t.UserAgent)
	}
	return req, nil
}

func (t Transport) fetchOnce(ctx context.Context, c *http.Client, url string, maxBytes int64) ([]byte, error) {
	req, err := t.request(ctx, url)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{URL: url, Status: resp.Status, Code: resp.StatusCode}
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: GET %s declared %d bytes, maximum is %d", ErrTooLarge, url, resp.ContentLength, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: GET %s exceeded %d bytes", ErrTooLarge, url, maxBytes)
	}
	return data, nil
}
