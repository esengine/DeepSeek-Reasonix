package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "broker-token-1"

// sessionRecorder wraps the fake so the handler-side context can be checked.
type sessionRecorder struct {
	*fakeExecutor
	sessions []string
}

func (s *sessionRecorder) Tabs(ctx context.Context) ([]Tab, error) {
	s.sessions = append(s.sessions, SessionFromContext(ctx))
	return s.fakeExecutor.Tabs(ctx)
}

func newRoundTrip(t *testing.T, exec Executor) (Executor, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(NewHTTPHandler(exec, testToken))
	t.Cleanup(srv.Close)
	return NewHTTPExecutor(srv.URL, testToken, srv.Client()), srv
}

func TestHTTPRoundTripEveryMethod(t *testing.T) {
	fake := &fakeExecutor{
		tabs:       []Tab{{ID: "t1", URL: "https://a.test", Title: "A", Loading: true}, {ID: "t2", URL: "https://b.test", Temporary: true}},
		snapshot:   Snapshot{DocumentToken: "doc-1", URL: "https://a.test", Title: "A", Tree: "root\n  button \"Go\" ref=e1", Refs: 1},
		screenshot: Screenshot{Path: "/tmp/shot.png", MIME: "image/png", Width: 800, Height: 600},
		act:        ActResult{Executed: true, DocumentToken: "doc-2", Outcome: OutcomeExecuted},
		downloads:  []Download{{ID: "d1", URL: "https://a.test/f.zip", Path: "/tmp/f.zip", State: "completed", Bytes: 42}},
	}
	rec := &sessionRecorder{fakeExecutor: fake}
	client, _ := newRoundTrip(t, rec)
	ctx := WithSession(context.Background(), "/sessions/one.jsonl")

	tabs, err := client.Tabs(ctx)
	if err != nil || !reflect.DeepEqual(tabs, fake.tabs) {
		t.Fatalf("tabs = %+v, %v; want %+v", tabs, err, fake.tabs)
	}
	if !reflect.DeepEqual(rec.sessions, []string{"/sessions/one.jsonl"}) {
		t.Fatalf("handler saw sessions %q, want the client's session", rec.sessions)
	}
	tab, err := client.Open(ctx, OpenRequest{OperationID: "open-1", URL: "https://c.test", Temporary: true})
	if err != nil || tab != (Tab{ID: "t-new", URL: "https://c.test", Temporary: true}) {
		t.Fatalf("open = %+v, %v", tab, err)
	}
	if !reflect.DeepEqual(fake.opens, []OpenRequest{{OperationID: "open-1", URL: "https://c.test", Temporary: true}}) {
		t.Fatalf("open request = %+v", fake.opens)
	}
	tab, err = client.Navigate(ctx, NavigateRequest{OperationID: "nav-1", TabID: "t1", URL: "https://d.test", Action: NavigateURL})
	if err != nil || tab != (Tab{ID: "t1", URL: "https://d.test"}) {
		t.Fatalf("navigate = %+v, %v", tab, err)
	}
	if !reflect.DeepEqual(fake.navs, []NavigateRequest{{OperationID: "nav-1", TabID: "t1", URL: "https://d.test", Action: NavigateURL}}) {
		t.Fatalf("navigate request = %+v", fake.navs)
	}
	snap, err := client.Snapshot(ctx, SnapshotRequest{TabID: "t1", Selector: "main"})
	if err != nil || snap != fake.snapshot {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
	if !reflect.DeepEqual(fake.snaps, []SnapshotRequest{{TabID: "t1", Selector: "main"}}) {
		t.Fatalf("snapshot request = %+v", fake.snaps)
	}
	shot, err := client.Screenshot(ctx, ScreenshotRequest{TabID: "t1", Ref: "e1", FullPage: true})
	if err != nil || shot != fake.screenshot {
		t.Fatalf("screenshot = %+v, %v", shot, err)
	}
	if !reflect.DeepEqual(fake.shots, []ScreenshotRequest{{TabID: "t1", Ref: "e1", FullPage: true}}) {
		t.Fatalf("screenshot request = %+v", fake.shots)
	}
	actReq := ActRequest{
		OperationID: "op-1", TabID: "t1", DocumentToken: "doc-1", Action: ActionUpload, Ref: "e1",
		Text: "hello", Keys: "Enter", Options: []string{"a", "b"}, Files: []string{"/tmp/x.txt"}, Submit: true, DeltaX: 3, DeltaY: -4,
	}
	res, err := client.Act(ctx, actReq)
	if err != nil || res != fake.act {
		t.Fatalf("act = %+v, %v", res, err)
	}
	if !reflect.DeepEqual(fake.acts, []ActRequest{actReq}) {
		t.Fatalf("act request = %+v, want %+v", fake.acts, actReq)
	}
	downloads, err := client.Downloads(ctx, DownloadsRequest{TabID: "t1", WaitFor: 1500 * time.Millisecond})
	if err != nil || !reflect.DeepEqual(downloads, fake.downloads) {
		t.Fatalf("downloads = %+v, %v", downloads, err)
	}
	if !reflect.DeepEqual(fake.dls, []DownloadsRequest{{TabID: "t1", WaitFor: 1500 * time.Millisecond}}) {
		t.Fatalf("downloads request = %+v", fake.dls)
	}
	if err := client.Close(ctx, CloseRequest{OperationID: "close-1", TabID: "t2"}); err != nil || !reflect.DeepEqual(fake.closed, []string{"t2"}) {
		t.Fatalf("close: err=%v closed=%v", err, fake.closed)
	}
	if len(fake.closes) != 1 || fake.closes[0].OperationID != "close-1" {
		t.Fatalf("close operationId lost: %+v", fake.closes)
	}
}

func TestHTTPEveryWriteTreatsMalformedReceiptAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{broken")) }))
	defer server.Close()
	exec := NewHTTPExecutor(server.URL, "token", server.Client())
	ctx := context.Background()
	checks := []error{}
	_, err := exec.Open(ctx, OpenRequest{OperationID: "open", URL: "https://example.test"})
	checks = append(checks, err)
	_, err = exec.Navigate(ctx, NavigateRequest{OperationID: "nav", TabID: "t", Action: NavigateBack})
	checks = append(checks, err)
	_, err = exec.Act(ctx, ActRequest{OperationID: "act", TabID: "t", Action: ActionClick})
	checks = append(checks, err)
	for _, err := range checks {
		if !errors.Is(err, ErrUnknownOutcome) {
			t.Fatalf("malformed write receipt: %v", err)
		}
	}
}

func TestHTTPActRequiresAnExplicitExecutionReceipt(t *testing.T) {
	for _, payload := range []string{`{}`, `{"executed":null}`, `{"executed":"false"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
		exec := NewHTTPExecutor(server.URL, "token", server.Client())
		_, err := exec.Act(context.Background(), ActRequest{OperationID: "act", Action: ActionClick})
		server.Close()
		if !errors.Is(err, ErrUnknownOutcome) {
			t.Fatalf("payload %s became a known receipt: %v", payload, err)
		}
	}
}

func TestHTTPRoundTripMapsEverySentinel(t *testing.T) {
	for _, sentinel := range []error{ErrStaleReference, ErrTakenOver, ErrNoGrant, ErrUnknownOutcome} {
		fake := &fakeExecutor{err: fmt.Errorf("%w: from the host", sentinel)}
		client, _ := newRoundTrip(t, fake)
		ctx := context.Background()
		checks := []struct {
			name string
			err  error
		}{
			{"tabs", func() error { _, err := client.Tabs(ctx); return err }()},
			{"open", func() error { _, err := client.Open(ctx, OpenRequest{URL: "https://a.test"}); return err }()},
			{"navigate", func() error {
				_, err := client.Navigate(ctx, NavigateRequest{TabID: "t1", Action: NavigateBack})
				return err
			}()},
			{"snapshot", func() error { _, err := client.Snapshot(ctx, SnapshotRequest{TabID: "t1"}); return err }()},
			{"screenshot", func() error { _, err := client.Screenshot(ctx, ScreenshotRequest{TabID: "t1"}); return err }()},
			{"act", func() error {
				_, err := client.Act(ctx, ActRequest{OperationID: "op", TabID: "t1", Action: ActionClick})
				return err
			}()},
			{"downloads", func() error { _, err := client.Downloads(ctx, DownloadsRequest{TabID: "t1"}); return err }()},
			{"close", client.Close(ctx, CloseRequest{OperationID: "close-1", TabID: "t1"})},
		}
		for _, c := range checks {
			if !errors.Is(c.err, sentinel) {
				t.Errorf("%s with %v: got %v, want the sentinel", c.name, sentinel, c.err)
			}
			if c.err != nil && c.err.Error() != sentinel.Error()+": from the host" {
				t.Errorf("%s with %v: message %q lost the host's detail", c.name, sentinel, c.err)
			}
		}
	}
}

func TestHTTPRoundTripPlainErrorsAndStatuses(t *testing.T) {
	fake := &fakeExecutor{err: errors.New("shell exploded")}
	client, srv := newRoundTrip(t, fake)
	if _, err := client.Tabs(context.Background()); err == nil || errors.Is(err, ErrNoGrant) || errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("plain error must not map onto a sentinel: %v", err)
	} else if got := err.Error(); got != "browser broker: tabs: status 500: shell exploded" {
		t.Fatalf("plain error text = %q", got)
	}
	wrong := NewHTTPExecutor(srv.URL, "other-token", srv.Client())
	if _, err := wrong.Tabs(context.Background()); err == nil || errors.Is(err, ErrNoGrant) {
		t.Fatalf("wrong token: got %v, want a plain 401 error", err)
	}
	if a, ok := wrong.(Availability); !ok || a.Available(context.Background()) {
		t.Fatal("wrong token must not be reported available")
	}
	resp, err := http.Get(srv.URL + "/v1/browser/tabs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer: status %d, want 401", resp.StatusCode)
	}
}

// TestHTTPUnknownOutcomeIsNeverRetried covers both ways a receipt is lost: a
// 409 unknown_outcome from the host, and a connection that dies before any
// reply. Either way exactly one request reaches the server.
func TestHTTPUnknownOutcomeIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	fake := &fakeExecutor{err: ErrUnknownOutcome}
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		NewHTTPHandler(fake, testToken).ServeHTTP(w, r)
	})
	srv := httptest.NewServer(counted)
	defer srv.Close()
	client := NewHTTPExecutor(srv.URL, testToken, srv.Client())
	res, err := client.Act(context.Background(), ActRequest{OperationID: "op-1", TabID: "t1", Action: ActionClick})
	if !errors.Is(err, ErrUnknownOutcome) || res.Outcome != OutcomeUnknown {
		t.Fatalf("act = %+v, %v; want ErrUnknownOutcome", res, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("unknown outcome reached the server %d times, want exactly 1", n)
	}

	var dropped atomic.Int32
	dropping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dropped.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.(*net.TCPConn).SetLinger(0)
		_ = conn.Close()
	}))
	defer dropping.Close()
	client = NewHTTPExecutor(dropping.URL, testToken, dropping.Client())
	res, err = client.Act(context.Background(), ActRequest{OperationID: "op-2", TabID: "t1", Action: ActionClick})
	if !errors.Is(err, ErrUnknownOutcome) || res.Outcome != OutcomeUnknown {
		t.Fatalf("dropped act = %+v, %v; want ErrUnknownOutcome", res, err)
	}
	if n := dropped.Load(); n != 1 {
		t.Fatalf("dropped act reached the server %d times, want exactly 1", n)
	}
	if _, err := client.Tabs(context.Background()); err == nil || errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("a dropped read is a plain error, got %v", err)
	}
}

func TestHTTPAvailableCachesHealthForThirtySeconds(t *testing.T) {
	var probes atomic.Int32
	gate := gatedExecutor{fakeExecutor: &fakeExecutor{}, available: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == httpHealthRoute {
			probes.Add(1)
		}
		NewHTTPHandler(gate, testToken).ServeHTTP(w, r)
	}))
	defer srv.Close()
	exec := NewHTTPExecutor(srv.URL, testToken, srv.Client()).(*httpExecutor)
	now := time.Unix(1_700_000_000, 0)
	exec.now = func() time.Time { return now }
	ctx := context.Background()
	for range 3 {
		if !exec.Available(ctx) {
			t.Fatal("healthy broker reported unavailable")
		}
	}
	if n := probes.Load(); n != 1 {
		t.Fatalf("health probed %d times within the cache window, want 1", n)
	}
	now = now.Add(httpHealthTTL)
	if !exec.Available(ctx) || probes.Load() != 2 {
		t.Fatalf("expired cache must probe again: probes=%d", probes.Load())
	}
	gate.available = false
	now = now.Add(httpHealthTTL)
	if exec.Available(ctx) {
		t.Fatal("503 health must report unavailable")
	}
	if exec.Available(ctx) || probes.Load() != 4 {
		t.Fatalf("a failed probe is not cached: probes=%d", probes.Load())
	}
}

func TestHTTPHandlerRejectsOversizedAndMalformedBodies(t *testing.T) {
	fake := &fakeExecutor{}
	srv := httptest.NewServer(NewHTTPHandler(fake, testToken))
	defer srv.Close()
	do := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/browser/open", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := do("{not json"); code != http.StatusBadRequest {
		t.Fatalf("malformed body: status %d, want 400", code)
	}
	big := make([]byte, httpMaxRequestBytes+16)
	for i := range big {
		big[i] = ' '
	}
	if code := do(`{"url":"` + string(big) + `"}`); code != http.StatusBadRequest {
		t.Fatalf("oversized body: status %d, want 400", code)
	}
	if len(fake.opens) != 0 {
		t.Fatalf("rejected bodies reached the executor: %+v", fake.opens)
	}
}

func TestSessionContextRoundTrip(t *testing.T) {
	if got := SessionFromContext(context.Background()); got != "" {
		t.Fatalf("empty context session = %q", got)
	}
	ctx := WithSession(context.Background(), "s-1")
	if got := SessionFromContext(ctx); got != "s-1" {
		t.Fatalf("session = %q, want s-1", got)
	}
	if WithSession(ctx, "") != ctx {
		t.Fatal("an empty session must not replace the context")
	}
}
