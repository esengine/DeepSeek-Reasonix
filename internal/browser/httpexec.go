package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const httpHealthTTL = 30 * time.Second

// httpExecutor is the JSON-over-HTTP client half of the contract. It never
// retries: a write whose reply was lost is reported as ErrUnknownOutcome, and
// the tools tell the model not to try again.
type httpExecutor struct {
	endpoint string
	token    string
	client   *http.Client
	now      func() time.Time

	mu        sync.Mutex
	healthyAt time.Time
}

// NewHTTPExecutor returns an Executor that forwards every call to the
// broker at endpoint with a bearer token. A nil client uses
// http.DefaultClient; the caller decides timeouts through ctx.
func NewHTTPExecutor(endpoint, token string, client *http.Client) Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpExecutor{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:    strings.TrimSpace(token),
		client:   client,
		now:      time.Now,
	}
}

// Available reports whether a health probe succeeded within the last 30 s,
// probing again when the cache is cold or expired.
func (e *httpExecutor) Available(ctx context.Context) bool {
	e.mu.Lock()
	fresh := !e.healthyAt.IsZero() && e.now().Sub(e.healthyAt) < httpHealthTTL
	e.mu.Unlock()
	if fresh {
		return true
	}
	req, err := e.newRequest(ctx, http.MethodGet, e.endpoint+httpHealthRoute, nil)
	if err != nil {
		return false
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return false
	}
	e.mu.Lock()
	e.healthyAt = e.now()
	e.mu.Unlock()
	return true
}

func (e *httpExecutor) newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := SessionFromContext(ctx); id != "" {
		req.Header.Set(SessionHeader, id)
	}
	return req, nil
}

// call posts in as JSON to /v1/browser/<method> and decodes the reply into
// out. Transport failures (no HTTP reply at all) come back as errTransport
// so Act can turn them into ErrUnknownOutcome.
func (e *httpExecutor) call(ctx context.Context, method string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("browser broker: encode %s: %w", method, err)
	}
	req, err := e.newRequest(ctx, http.MethodPost, e.endpoint+httpRoutePrefix+method, body)
	if err != nil {
		return fmt.Errorf("browser broker: %s: %w", method, err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return &transportError{method: method, err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxResponseBytes+1))
	if err != nil {
		return &transportError{method: method, err: err}
	}
	if len(data) > httpMaxResponseBytes {
		return fmt.Errorf("browser broker: %s: reply exceeds %d bytes", method, httpMaxResponseBytes)
	}
	if resp.StatusCode == http.StatusConflict {
		return decodeWireError(method, data)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("browser broker: %s: status %d: %s", method, resp.StatusCode, wireMessage(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("browser broker: decode %s reply: %w", method, err)
	}
	return nil
}

type transportError struct {
	method string
	err    error
}

func (t *transportError) Error() string { return "browser broker: " + t.method + ": " + t.err.Error() }
func (t *transportError) Unwrap() error { return t.err }

// wireMessage prefers the handler's message over a raw body dump.
func wireMessage(data []byte) string {
	var we wireError
	if err := json.Unmarshal(data, &we); err == nil && we.Message != "" {
		return we.Message
	}
	return strings.TrimSpace(string(data))
}

func decodeWireError(method string, data []byte) error {
	var we wireError
	if err := json.Unmarshal(data, &we); err != nil || we.Error == "" {
		return fmt.Errorf("browser broker: %s: status 409: %s", method, strings.TrimSpace(string(data)))
	}
	sentinel, ok := wireErrorCodes[we.Error]
	if !ok {
		return fmt.Errorf("browser broker: %s: %s: %s", method, we.Error, we.Message)
	}
	detail := strings.TrimPrefix(strings.TrimPrefix(we.Message, sentinel.Error()), ": ")
	if detail == "" {
		return sentinel
	}
	return fmt.Errorf("%w: %s", sentinel, detail)
}

func (e *httpExecutor) Tabs(ctx context.Context) ([]Tab, error) {
	var out wireTabs
	if err := e.call(ctx, "tabs", struct{}{}, &out); err != nil {
		return nil, err
	}
	tabs := make([]Tab, 0, len(out.Tabs))
	for _, t := range out.Tabs {
		tabs = append(tabs, t.tab())
	}
	return tabs, nil
}

func (e *httpExecutor) Open(ctx context.Context, req OpenRequest) (Tab, error) {
	var out wireTab
	if err := e.write(ctx, "open", wireOpenRequest(req), &out); err != nil {
		return Tab{}, err
	}
	return out.tab(), nil
}

func (e *httpExecutor) Navigate(ctx context.Context, req NavigateRequest) (Tab, error) {
	var out wireTab
	if err := e.write(ctx, "navigate", wireNavigateRequest(req), &out); err != nil {
		return Tab{}, err
	}
	return out.tab(), nil
}

func (e *httpExecutor) Snapshot(ctx context.Context, req SnapshotRequest) (Snapshot, error) {
	var out wireSnapshot
	if err := e.call(ctx, "snapshot", wireSnapshotRequest(req), &out); err != nil {
		return Snapshot{}, err
	}
	return Snapshot(out), nil
}

func (e *httpExecutor) Screenshot(ctx context.Context, req ScreenshotRequest) (Screenshot, error) {
	var out wireScreenshot
	if err := e.call(ctx, "screenshot", wireScreenshotRequest(req), &out); err != nil {
		return Screenshot{}, err
	}
	return Screenshot(out), nil
}

// Act sends one reserved write. A reply that never arrived leaves the
// action's fate unknown, which is exactly ErrUnknownOutcome.
func (e *httpExecutor) Act(ctx context.Context, req ActRequest) (ActResult, error) {
	var out wireActResult
	if err := e.write(ctx, "act", toWireAct(req), &out); err != nil {
		if errors.Is(err, ErrUnknownOutcome) {
			return ActResult{Outcome: OutcomeUnknown}, err
		}
		return ActResult{}, err
	}
	res := ActResult(out)
	if res.Outcome == "" {
		res.Outcome = OutcomeNotExecuted
		if res.Executed {
			res.Outcome = OutcomeExecuted
		}
	}
	return res, nil
}

func (e *httpExecutor) Downloads(ctx context.Context, req DownloadsRequest) ([]Download, error) {
	var out wireDownloads
	in := wireDownloadsRequest{TabID: req.TabID, WaitForMs: req.WaitFor.Milliseconds()}
	if err := e.call(ctx, "downloads", in, &out); err != nil {
		return nil, err
	}
	downloads := make([]Download, 0, len(out.Downloads))
	for _, d := range out.Downloads {
		downloads = append(downloads, Download(d))
	}
	return downloads, nil
}

func (e *httpExecutor) Close(ctx context.Context, req CloseRequest) error {
	return e.write(ctx, "close", wireCloseRequest(req), nil)
}

// Once a write is handed to HTTP, only explicit refusal codes prove it did
// not run. Truncated/invalid replies and HTTP failures also leave it unknown.
func (e *httpExecutor) write(ctx context.Context, method string, in, out any) error {
	err := e.call(ctx, method, in, out)
	if err == nil || errors.Is(err, ErrStaleReference) || errors.Is(err, ErrTakenOver) || errors.Is(err, ErrNoGrant) || errors.Is(err, ErrUnknownOutcome) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrUnknownOutcome, err.Error())
}
