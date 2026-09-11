package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/browser"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

// brokerTestExecutor records the session header each call arrived with.
type brokerTestExecutor struct {
	sessions []string
	tabs     []browser.Tab
}

func (e *brokerTestExecutor) session(ctx context.Context) string {
	id := browser.SessionFromContext(ctx)
	e.sessions = append(e.sessions, id)
	return id
}

func (e *brokerTestExecutor) Tabs(ctx context.Context) ([]browser.Tab, error) {
	e.session(ctx)
	return e.tabs, nil
}
func (e *brokerTestExecutor) Open(ctx context.Context, req browser.OpenRequest) (browser.Tab, error) {
	e.session(ctx)
	return browser.Tab{ID: "t1", URL: req.URL}, nil
}
func (e *brokerTestExecutor) Navigate(ctx context.Context, req browser.NavigateRequest) (browser.Tab, error) {
	e.session(ctx)
	return browser.Tab{ID: req.TabID}, nil
}
func (e *brokerTestExecutor) Snapshot(ctx context.Context, _ browser.SnapshotRequest) (browser.Snapshot, error) {
	e.session(ctx)
	return browser.Snapshot{}, nil
}
func (e *brokerTestExecutor) Screenshot(ctx context.Context, _ browser.ScreenshotRequest) (browser.Screenshot, error) {
	e.session(ctx)
	return browser.Screenshot{}, nil
}
func (e *brokerTestExecutor) Act(ctx context.Context, req browser.ActRequest) (browser.ActResult, error) {
	e.session(ctx)
	return browser.ActResult{Executed: true, Outcome: browser.OutcomeExecuted}, nil
}
func (e *brokerTestExecutor) Downloads(ctx context.Context, _ browser.DownloadsRequest) ([]browser.Download, error) {
	e.session(ctx)
	return nil, nil
}
func (e *brokerTestExecutor) Close(ctx context.Context, _ browser.CloseRequest) error {
	e.session(ctx)
	return nil
}

func newBrokerTestHost(t *testing.T, token string) (*httptest.Server, *brokerTestExecutor) {
	t.Helper()
	exec := &brokerTestExecutor{tabs: []browser.Tab{{ID: "t1", URL: "https://example.test"}}}
	srv := httptest.NewServer(browser.NewHTTPHandler(exec, token))
	t.Cleanup(srv.Close)
	return srv, exec
}

func TestNewBrowserBrokerValidatesEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"", "http://example.com:1", "https://127.0.0.1:1", "http://127.0.0.1",
		"http://127.0.0.1:1/path", "http://user@127.0.0.1:1", "http://10.0.0.1:9",
	} {
		if _, err := NewBrowserBroker(endpoint, "tok"); err == nil {
			t.Fatalf("endpoint %q accepted, want rejection", endpoint)
		}
	}
	if _, err := NewBrowserBroker("http://127.0.0.1:9999", " "); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err := NewBrowserBroker("http://127.0.0.1:9999", "tok"); err != nil {
		t.Fatalf("valid loopback endpoint rejected: %v", err)
	}
}

func TestBrowserBrokerSessionScopeTravelsOverHTTP(t *testing.T) {
	host, exec := newBrokerTestHost(t, "tok")
	broker, err := NewBrowserBroker(host.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	tag := newSessionTagSink(bc)
	tag.SetPath("/remote/sessions/a.jsonl")
	scoped := broker.ForSession(tag)
	tabs, err := scoped.Tabs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != "t1" {
		t.Fatalf("tabs = %+v", tabs)
	}
	if want := agent.CanonicalSessionPath("/remote/sessions/a.jsonl"); len(exec.sessions) != 1 || exec.sessions[0] != want {
		t.Fatalf("host saw sessions %v, want the tag path %q", exec.sessions, want)
	}
}

func TestBrowserBrokerRebindRotatesGeneration(t *testing.T) {
	first, _ := newBrokerTestHost(t, "tok-1")
	second, secondExec := newBrokerTestHost(t, "tok-2")
	broker, err := NewBrowserBroker(first.URL, "tok-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Rebind(second.URL, "tok-2"); err != nil {
		t.Fatal(err)
	}
	if broker.Endpoint() != second.URL {
		t.Fatalf("endpoint = %q, want %q", broker.Endpoint(), second.URL)
	}
	tabs, err := broker.Tabs(context.Background())
	if err != nil || len(tabs) != 1 {
		t.Fatalf("Tabs after rebind = %+v, %v", tabs, err)
	}
	if len(secondExec.sessions) != 1 {
		t.Fatalf("second generation saw %d calls, want 1", len(secondExec.sessions))
	}
	if err := broker.Rebind("http://192.0.2.1:9", "tok"); err == nil {
		t.Fatal("rebind to a non-loopback endpoint accepted")
	}
}

func newBrokerTestServer(t *testing.T, opts boot.Options) *Server {
	t.Helper()
	dir := t.TempDir()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, WorkspaceRoot: dir})
	t.Cleanup(func() { ctrl.Close() })
	srv := New(ctrl, bc, config.ServeConfig{})
	srv.SetControllerBuildOptions(opts)
	return srv
}

func TestServerCapabilitiesFollowBroker(t *testing.T) {
	if caps := newBrokerTestServer(t, boot.Options{}).capabilities(); len(caps) != 0 {
		t.Fatalf("capabilities without broker = %v", caps)
	}
	broker, err := NewBrowserBroker("http://127.0.0.1:9999", "tok")
	if err != nil {
		t.Fatal(err)
	}
	srv := newBrokerTestServer(t, boot.Options{BrowserExecutor: broker})
	caps := srv.capabilities()
	if len(caps) != 1 || caps[0] != capabilityBrowser {
		t.Fatalf("capabilities with broker = %v", caps)
	}
}

func TestBrowserBrokerRebindHTTP(t *testing.T) {
	// No broker configured: the route refuses instead of inventing one.
	srv := newBrokerTestServer(t, boot.Options{})
	rec := httptest.NewRecorder()
	srv.browserBrokerRebind(rec, httptest.NewRequest(http.MethodPost, "/browser/broker",
		strings.NewReader(`{"endpoint":"http://127.0.0.1:9","token":"t"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("rebind without broker = %d, want 409", rec.Code)
	}

	broker, err := NewBrowserBroker("http://127.0.0.1:9999", "old")
	if err != nil {
		t.Fatal(err)
	}
	srv = newBrokerTestServer(t, boot.Options{BrowserExecutor: broker})
	rec = httptest.NewRecorder()
	srv.browserBrokerRebind(rec, httptest.NewRequest(http.MethodPost, "/browser/broker", strings.NewReader(`{`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rebind with malformed body = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.browserBrokerRebind(rec, httptest.NewRequest(http.MethodPost, "/browser/broker",
		strings.NewReader(`{"endpoint":"http://127.0.0.1:1234","token":"new"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("rebind = %d, want 204", rec.Code)
	}
	if broker.Endpoint() != "http://127.0.0.1:1234" {
		t.Fatalf("endpoint after rebind = %q", broker.Endpoint())
	}
}

func TestHandshakeAdvertisesBrowserCapability(t *testing.T) {
	broker, err := NewBrowserBroker("http://127.0.0.1:9999", "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, withBroker := range []bool{false, true} {
		opts := boot.Options{}
		if withBroker {
			opts.BrowserExecutor = broker
		}
		dir := t.TempDir()
		bc := NewBroadcaster()
		ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, WorkspaceRoot: dir})
		defer ctrl.Close()
		srv := New(ctrl, bc, config.ServeConfig{AuthMode: "token", Token: "secret"})
		srv.SetControllerBuildOptions(opts)
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		resp, err := http.Post(ts.URL+"/auth/token", "application/json", strings.NewReader(`{"token":"secret"}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("handshake status = %d, want 204", resp.StatusCode)
		}
		got := resp.Header.Get(capabilitiesHeader)
		if withBroker && got != capabilityBrowser {
			t.Fatalf("capabilities header = %q, want %q", got, capabilityBrowser)
		}
		if !withBroker && got != "" {
			t.Fatalf("capabilities header = %q, want empty", got)
		}
	}
}

func TestSessionBrowserExecutorPassthrough(t *testing.T) {
	exec := &brokerTestExecutor{}
	srv := newBrokerTestServer(t, boot.Options{BrowserExecutor: exec})
	if got := srv.sessionBrowserExecutor(nil); got != browser.Executor(exec) {
		t.Fatalf("non-broker executor wrapped: %T", got)
	}
	if srv.browserBroker() != nil {
		t.Fatal("browserBroker() non-nil without a broker")
	}
}

func TestBuildTaggedScopesBrokerToSession(t *testing.T) {
	host, hostExec := newBrokerTestHost(t, "tok")
	broker, err := NewBrowserBroker(host.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	srv := newBrokerTestServer(t, boot.Options{BrowserExecutor: broker})
	var gotOpts boot.Options
	srv.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		gotOpts = opts
		return control.New(control.Options{Sink: opts.Sink, SessionDir: opts.SessionDir, WorkspaceRoot: opts.WorkspaceRoot}), nil
	}
	built, tag, err := srv.buildTagged(context.Background(), "provider/model", false)
	if err != nil {
		t.Fatal(err)
	}
	defer built.Close()
	tag.SetPath("/remote/sessions/b.jsonl")
	if _, ok := gotOpts.BrowserExecutor.(sessionBrowserExecutor); !ok {
		t.Fatalf("buildTagged BrowserExecutor = %T, want sessionBrowserExecutor", gotOpts.BrowserExecutor)
	}
	if _, err := gotOpts.BrowserExecutor.Tabs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := agent.CanonicalSessionPath("/remote/sessions/b.jsonl"); len(hostExec.sessions) == 0 || hostExec.sessions[len(hostExec.sessions)-1] != want {
		t.Fatalf("host saw sessions %v, want the built controller's tag path %q", hostExec.sessions, want)
	}
}
