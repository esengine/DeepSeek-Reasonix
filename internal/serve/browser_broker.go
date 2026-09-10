package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"reasonix/internal/browser"
)

// Capability tokens advertised on the /auth/token handshake reply so a
// desktop can tell what this serve supports without a second round trip.
const (
	capabilitiesHeader = "X-Reasonix-Serve-Capabilities"
	capabilityBrowser  = "browser"
)

// BrowserBroker is Serve's end of the desktop browser broker: one HTTP
// executor whose endpoint and token follow the SSH connection generation. The
// desktop rebinds it after a reconnect through POST /browser/broker, so the
// controllers built around it never need a rebuild to keep their browser.
type BrowserBroker struct {
	mu       sync.Mutex
	exec     browser.Executor
	endpoint string
}

// NewBrowserBroker dials the broker at endpoint, which must be a loopback
// http URL: the token authorises browser writes and must not leave the host.
func NewBrowserBroker(endpoint, token string) (*BrowserBroker, error) {
	b := &BrowserBroker{}
	if err := b.Rebind(endpoint, token); err != nil {
		return nil, err
	}
	return b, nil
}

// Rebind points every session at a new endpoint and token; the previous
// generation's health cache is dropped with the executor that held it.
func (b *BrowserBroker) Rebind(endpoint, token string) error {
	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if err := validateBrokerEndpoint(endpoint); err != nil {
		return err
	}
	if token == "" {
		return errors.New("browser broker: token is required")
	}
	b.mu.Lock()
	b.exec = browser.NewHTTPExecutor(endpoint, token, nil)
	b.endpoint = endpoint
	b.mu.Unlock()
	return nil
}

// Endpoint reports the current broker URL.
func (b *BrowserBroker) Endpoint() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.endpoint
}

func (b *BrowserBroker) current() browser.Executor {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exec
}

// BrowserBroker itself satisfies browser.Executor by delegating to the current
// generation without a session scope; controllers should prefer the scoped
// view from ForSession, and these methods exist so the broker can sit in
// boot.Options.BrowserExecutor until one is derived.
func (b *BrowserBroker) Available(ctx context.Context) bool {
	exec := b.current()
	if a, ok := exec.(browser.Availability); ok {
		return a.Available(ctx)
	}
	return exec != nil
}

func (b *BrowserBroker) Tabs(ctx context.Context) ([]browser.Tab, error) {
	return b.current().Tabs(ctx)
}

func (b *BrowserBroker) Open(ctx context.Context, req browser.OpenRequest) (browser.Tab, error) {
	return b.current().Open(ctx, req)
}

func (b *BrowserBroker) Navigate(ctx context.Context, req browser.NavigateRequest) (browser.Tab, error) {
	return b.current().Navigate(ctx, req)
}

func (b *BrowserBroker) Snapshot(ctx context.Context, req browser.SnapshotRequest) (browser.Snapshot, error) {
	return b.current().Snapshot(ctx, req)
}

func (b *BrowserBroker) Screenshot(ctx context.Context, req browser.ScreenshotRequest) (browser.Screenshot, error) {
	return b.current().Screenshot(ctx, req)
}

func (b *BrowserBroker) Act(ctx context.Context, req browser.ActRequest) (browser.ActResult, error) {
	return b.current().Act(ctx, req)
}

func (b *BrowserBroker) Downloads(ctx context.Context, req browser.DownloadsRequest) ([]browser.Download, error) {
	return b.current().Downloads(ctx, req)
}

func (b *BrowserBroker) Close(ctx context.Context, req browser.CloseRequest) error {
	return b.current().Close(ctx, req)
}

func validateBrokerEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("browser broker: endpoint %q must be a plain http loopback URL", endpoint)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("browser broker: endpoint %q must point at a loopback address", endpoint)
	}
	if u.Port() == "" {
		return fmt.Errorf("browser broker: endpoint %q must carry a port", endpoint)
	}
	return nil
}

// ForSession returns the Executor one controller builds around: every call
// is scoped to the session the tag currently routes, so the desktop broker
// can bind the request to exactly one desktop tab.
func (b *BrowserBroker) ForSession(tag *SessionTagSink) browser.Executor {
	if b == nil {
		return nil
	}
	return sessionBrowserExecutor{broker: b, tag: tag}
}

// sessionBrowserExecutor is a per-controller view over the shared broker.
type sessionBrowserExecutor struct {
	broker *BrowserBroker
	tag    *sessionTagSink
}

func (s sessionBrowserExecutor) scope(ctx context.Context) (context.Context, browser.Executor) {
	if s.tag != nil {
		ctx = browser.WithSession(ctx, s.tag.Path())
	}
	return ctx, s.broker.current()
}

func (s sessionBrowserExecutor) Available(ctx context.Context) bool {
	ctx, exec := s.scope(ctx)
	if a, ok := exec.(browser.Availability); ok {
		return a.Available(ctx)
	}
	return exec != nil
}

func (s sessionBrowserExecutor) Tabs(ctx context.Context) ([]browser.Tab, error) {
	ctx, exec := s.scope(ctx)
	return exec.Tabs(ctx)
}

func (s sessionBrowserExecutor) Open(ctx context.Context, req browser.OpenRequest) (browser.Tab, error) {
	ctx, exec := s.scope(ctx)
	return exec.Open(ctx, req)
}

func (s sessionBrowserExecutor) Navigate(ctx context.Context, req browser.NavigateRequest) (browser.Tab, error) {
	ctx, exec := s.scope(ctx)
	return exec.Navigate(ctx, req)
}

func (s sessionBrowserExecutor) Snapshot(ctx context.Context, req browser.SnapshotRequest) (browser.Snapshot, error) {
	ctx, exec := s.scope(ctx)
	return exec.Snapshot(ctx, req)
}

func (s sessionBrowserExecutor) Screenshot(ctx context.Context, req browser.ScreenshotRequest) (browser.Screenshot, error) {
	ctx, exec := s.scope(ctx)
	return exec.Screenshot(ctx, req)
}

func (s sessionBrowserExecutor) Act(ctx context.Context, req browser.ActRequest) (browser.ActResult, error) {
	ctx, exec := s.scope(ctx)
	return exec.Act(ctx, req)
}

func (s sessionBrowserExecutor) Downloads(ctx context.Context, req browser.DownloadsRequest) ([]browser.Download, error) {
	ctx, exec := s.scope(ctx)
	return exec.Downloads(ctx, req)
}

func (s sessionBrowserExecutor) Close(ctx context.Context, req browser.CloseRequest) error {
	ctx, exec := s.scope(ctx)
	return exec.Close(ctx, req)
}

// sessionBrowserExecutor binds the configured executor to one controller's
// tag. A broker gains the session scope; any other executor (tests, embedded
// hosts) is handed through untouched.
func (s *Server) sessionBrowserExecutor(tag *sessionTagSink) browser.Executor {
	if b, ok := s.buildOptions.BrowserExecutor.(*BrowserBroker); ok {
		return b.ForSession(tag)
	}
	return s.buildOptions.BrowserExecutor
}

func (s *Server) browserBroker() *BrowserBroker {
	b, _ := s.buildOptions.BrowserExecutor.(*BrowserBroker)
	return b
}

// capabilities lists what the handshake advertises to the desktop.
func (s *Server) capabilities() []string {
	var caps []string
	if s.buildOptions.BrowserExecutor != nil {
		caps = append(caps, capabilityBrowser)
	}
	return caps
}

// browserBrokerRebind lets the desktop hand a reused serve the broker of a
// new SSH connection generation: the reverse forward moved and the token
// rotated, and the process environment cannot follow.
func (s *Server) browserBrokerRebind(w http.ResponseWriter, r *http.Request) {
	broker := s.browserBroker()
	if broker == nil {
		http.Error(w, "this serve was started without a browser broker", http.StatusConflict)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Token    string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := broker.Rebind(body.Endpoint, body.Token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
