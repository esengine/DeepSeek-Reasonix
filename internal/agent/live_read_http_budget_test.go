//go:build live

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type liveReadBudgetKey struct{}

// The HTTP boundary counts every upstream attempt, including adapter retries.
// Unknown usage retains a conservative full-window reservation. Nothing here
// stores or logs Authorization, request bodies, or source text.
type liveReadBudget struct {
	mu               sync.Mutex
	requests, tokens int
	cancel           context.CancelFunc
	transport        http.RoundTripper
}

const liveReadRequestReservation = 128_000

func (b *liveReadBudget) admit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests < 600 && b.tokens+liveReadRequestReservation <= 3_000_000
}

func (b *liveReadBudget) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	if b.requests >= 600 || b.tokens+liveReadRequestReservation > 3_000_000 {
		b.mu.Unlock()
		b.cancel()
		http.Error(w, "live suite resource ceiling", http.StatusForbidden)
		return
	}
	b.requests++
	b.tokens += liveReadRequestReservation
	b.mu.Unlock()
	// The test enforces a real output bound even when the production adapter
	// deliberately omits max_tokens for a shared context/output window.
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "cannot read test request", 400)
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		http.Error(w, "invalid test request", 400)
		return
	}
	fields["max_tokens"] = json.RawMessage("2048")
	body, err = json.Marshal(fields)
	if err != nil {
		http.Error(w, "cannot encode test request", 400)
		return
	}
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "request creation failed", 500)
		return
	}
	upstream.Header.Set("Authorization", r.Header.Get("Authorization"))
	upstream.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: b.transport, Timeout: 150 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(upstream)
	if err != nil {
		http.Error(w, "upstream request failed", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == 401 || response.StatusCode == 403 {
			b.cancel()
		}
		http.Error(w, "upstream rejected live test request", response.StatusCode)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	used := 0
	defer func() {
		// The client can finish immediately after the final usage frame and
		// close before [DONE]. Account that known usage even if Write fails.
		if used > 0 {
			b.mu.Lock()
			b.tokens += used - liveReadRequestReservation
			b.mu.Unlock()
		}
	}()
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.HasPrefix(line, []byte("data:")) {
			var frame struct {
				Usage *struct {
					Prompt     int `json:"prompt_tokens"`
					Completion int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &frame) == nil && frame.Usage != nil {
				used = max(used, frame.Usage.Prompt+frame.Usage.Completion)
			}
		}
		if _, err := w.Write(append(bytes.Clone(line), '\n')); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

type liveBudgetTransport func(*http.Request) (*http.Response, error)

func (f liveBudgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type closedLiveWriter struct{ header http.Header }

func (w closedLiveWriter) Header() http.Header     { return w.header }
func (closedLiveWriter) WriteHeader(int)           {}
func (closedLiveWriter) Write([]byte) (int, error) { return 0, errors.New("client closed") }

func TestLiveReadHTTPBudgetAccountsUsageBeforeClientClose(t *testing.T) {
	budget := &liveReadBudget{cancel: func() { t.Error("unexpected cancellation") }}
	budget.transport = liveBudgetTransport(func(r *http.Request) (*http.Response, error) {
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal(err)
		}
		if fields["max_tokens"] != float64(2048) {
			t.Fatalf("output cap = %v", fields["max_tokens"])
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n"))}, nil
	})
	budget.ServeHTTP(closedLiveWriter{header: make(http.Header)}, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"messages":[]}`)))
	if budget.requests != 1 || budget.tokens != 120 {
		t.Fatalf("requests=%d tokens=%d", budget.requests, budget.tokens)
	}
}

func TestLiveReadHTTPBudgetKeepsUnknownReservationAndStops(t *testing.T) {
	cancelled := false
	budget := &liveReadBudget{cancel: func() { cancelled = true }, tokens: 2_800_000}
	budget.transport = liveBudgetTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("no usage available")
	})
	budget.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	if budget.tokens != 2_928_000 || budget.admit() {
		t.Fatalf("unknown reservation lost: %d", budget.tokens)
	}
	budget.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	if !cancelled || budget.requests != 1 {
		t.Fatalf("limit not enforced: cancelled=%v attempts=%d", cancelled, budget.requests)
	}
}
