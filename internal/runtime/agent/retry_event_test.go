package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessionstore"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type statusThenOKProvider struct {
	status  int
	timeout time.Duration
}

func (p *statusThenOKProvider) Name() string { return "status-then-ok" }

func (p *statusThenOKProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		code := http.StatusOK
		if calls == 1 {
			code = p.status
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	newReq := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid/", nil)
	}
	resp, err := provider.SendWithRetry(ctx, client, provider.SendOptions{Provider: "p", HeaderTimeout: p.timeout}, newReq)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestHeaderRetryEventCarriesCauseStatusDelayAndTimeout(t *testing.T) {
	sink := &recordSink{}
	a := New(&statusThenOKProvider{status: http.StatusBadGateway, timeout: 123 * time.Second}, echoRegistry(), sessionstore.NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 1 {
		t.Fatalf("retry events = %+v, want one", retries)
	}
	got := retries[0]
	if got.RetryScope != event.RetryScopeHeaders || got.RetryCause != provider.RetryCauseUpstreamStatus || got.RetryStatus != 502 {
		t.Fatalf("event = %+v, want headers/upstream_status/502", got)
	}
	if got.RetryDelayMs < 500 || got.RetryDelayMs >= 750 {
		t.Fatalf("delay = %dms, want the first backoff of 500-750ms", got.RetryDelayMs)
	}
	if got.RetryTimeoutSecs != 123 {
		t.Fatalf("timeout = %ds, want the provider's 123s", got.RetryTimeoutSecs)
	}
}

func TestStreamRetryEventCarriesCauseAndDelay(t *testing.T) {
	cases := map[string]provider.RetryCause{
		provider.StreamInterruptIdleTimeout:     provider.RetryCauseStreamIdle,
		provider.StreamInterruptPrematureEOF:    provider.RetryCauseConnectionClosed,
		provider.StreamInterruptConnectionReset: provider.RetryCauseConnectionClosed,
	}
	for reason, want := range cases {
		t.Run(reason, func(t *testing.T) {
			interrupted := &provider.StreamInterruptedError{Err: errors.New("cut"), Reason: reason}
			mp := testutil.NewMock("m", testutil.Turn{Text: "first ", ChunkError: interrupted}, testutil.Turn{Text: "done"})
			sink := &recordSink{}
			a := New(mp, echoRegistry(), sessionstore.NewSession(""), Options{}, sink)
			if err := a.Run(context.Background(), "go"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			retries := sink.kinds(event.Retrying)
			if len(retries) != 1 || retries[0].RetryScope != event.RetryScopeStream {
				t.Fatalf("retry events = %+v, want one stream retry", retries)
			}
			if retries[0].RetryCause != want {
				t.Fatalf("cause = %q, want %q", retries[0].RetryCause, want)
			}
			if retries[0].RetryDelayMs < 500 || retries[0].RetryDelayMs >= 750 {
				t.Fatalf("delay = %dms, want the first replay backoff of 500-750ms", retries[0].RetryDelayMs)
			}
			if retries[0].RetryStatus != 0 || retries[0].RetryTimeoutSecs != 0 {
				t.Fatalf("a stream replay has no status or header wait: %+v", retries[0])
			}
		})
	}
}

func TestStreamRetryDelayGrowsAndCaps(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 5; attempt++ {
		d := streamRetryDelay(attempt)
		if d < prev {
			t.Fatalf("delay for attempt %d (%s) shrank from %s", attempt, d, prev)
		}
		prev = d
	}
	if d := streamRetryDelay(50); d < 8*time.Second || d >= 8250*time.Millisecond {
		t.Fatalf("delay past the table = %s, want the 8s cap plus jitter", d)
	}
}
