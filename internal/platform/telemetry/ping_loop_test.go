package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/surface"
)

type fakeTime struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
	stop   func() bool
	cancel context.CancelFunc
	// limit bounds the waits a test may spend, so a loop that never reaches its
	// stop condition fails fast instead of spinning until the package timeout.
	limit int
	fail  func(string, ...any)
}

func (f *fakeTime) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeTime) sleep(ctx context.Context, d time.Duration) bool {
	f.mu.Lock()
	f.sleeps = append(f.sleeps, d)
	f.t = f.t.Add(d)
	stop := f.stop != nil && f.stop()
	if len(f.sleeps) >= f.limit {
		f.fail("loop made %d waits without reaching the test's stop condition", len(f.sleeps))
		stop = true
	}
	f.mu.Unlock()
	if stop {
		f.cancel()
	}
	return ctx.Err() == nil
}

type pingRecorder struct {
	mu     sync.Mutex
	bodies []string
	fail   int
}

func (r *pingRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail > 0 {
		r.fail--
		return nil, errors.New("offline")
	}
	r.bodies = append(r.bodies, string(b))
	return telemetryResponse(http.StatusAccepted), nil
}

func (r *pingRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func loopClient(t *testing.T, home string, rec *pingRecorder, start time.Time) (*Client, *fakeTime, context.Context) {
	clearPolicyEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	ft := &fakeTime{t: start, cancel: cancel, limit: 5000, fail: t.Errorf}
	c := testClient(home, rec)
	c.surface = surface.Studio
	c.now, c.sleep = ft.now, ft.sleep
	return c, ft, ctx
}

var day1 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestFailedPingIsRetriedWithinTheDayUntilItLands(t *testing.T) {
	rec := &pingRecorder{fail: 3}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	ft.stop = func() bool { return rec.count() == 1 }
	c.background(ctx, true)

	if rec.count() != 1 {
		t.Fatalf("landed pings = %d, want 1", rec.count())
	}
	if len(ft.sleeps) != 4 {
		t.Fatalf("waits = %v, want 3 retry waits then one wait for tomorrow", ft.sleeps)
	}
	for i, d := range ft.sleeps[:3] {
		if d < pingRetryBase*4/5 || d > pingRetryCap*6/5 {
			t.Fatalf("retry wait %d = %v out of bounds", i, d)
		}
	}
	if ft.sleeps[2] <= ft.sleeps[0] {
		t.Fatalf("retry waits do not back off: %v", ft.sleeps[:3])
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	for failures := 1; failures < 40; failures++ {
		d := retryDelay(failures)
		if d <= 0 || d > pingRetryCap*6/5 {
			t.Fatalf("retryDelay(%d) = %v", failures, d)
		}
	}
}

func TestOpenHostPingsAgainOnlyAfterUTCMidnight(t *testing.T) {
	home := testenv.TempDir(t)
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, home, rec, day1)
	ft.stop = func() bool { return rec.count() == 2 }
	c.background(ctx, true)

	if rec.count() != 2 {
		t.Fatalf("pings = %d, want 2", rec.count())
	}
	midnight := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if ft.t.Before(midnight) || ft.t.After(midnight.Add(rolloverSplay+maxWait)) {
		t.Fatalf("second ping at %v, want just after %v", ft.t, midnight)
	}
	for _, d := range ft.sleeps {
		if d > maxWait {
			t.Fatalf("wait %v exceeds the wake-up cap", d)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "cli-telemetry-ping-studio-2026-10-07")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("yesterday's claim survives: %v", err)
	}
}

func TestRestartOnTheSameDayDoesNotPingAgain(t *testing.T) {
	home := testenv.TempDir(t)
	rec := &pingRecorder{}
	first, ft, ctx := loopClient(t, home, rec, day1)
	ft.stop = func() bool { return true }
	first.background(ctx, true)

	second, ft2, ctx2 := loopClient(t, home, rec, day1.Add(3*time.Hour))
	ft2.stop = func() bool { return true }
	second.background(ctx2, true)

	if rec.count() != 1 {
		t.Fatalf("pings = %d, want 1 per install per day", rec.count())
	}
}

func TestPingHeldByAnotherProcessIsAskedAgainNotAssumedSent(t *testing.T) {
	home := testenv.TempDir(t)
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, home, rec, day1)
	claim := filepath.Join(home, "cli-telemetry-ping-studio-2026-10-07")
	if err := os.WriteFile(claim, []byte("sending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(claim, day1, day1); err != nil {
		t.Fatal(err)
	}
	ft.stop = func() bool { return rec.count() == 1 }
	c.background(ctx, true)
	if rec.count() != 1 || ft.sleeps[0] > pingRetryCap {
		t.Fatalf("pings = %d, first wait = %v", rec.count(), ft.sleeps)
	}
}

func TestSuppressedPingSendsNothingButStillDrains(t *testing.T) {
	home := testenv.TempDir(t)
	if err := appendPending(home, pendingPayload{
		Version: "v1.20.0", OS: "linux", Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	var paths []string
	c, ft, ctx := loopClient(t, home, nil, day1)
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return telemetryResponse(http.StatusAccepted), nil
	})}
	c.background(ctx, false)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/metrics") || len(ft.sleeps) != 0 {
		t.Fatalf("requests = %v, waits = %v", paths, ft.sleeps)
	}
}

func TestShutdownEndsTheLoopAndAbortsTheRequestInFlight(t *testing.T) {
	clearPolicyEnv(t)
	started := make(chan struct{})
	c := testClient(testenv.TempDir(t), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.background(ctx, true); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background outlived its context")
	}
	if matches, _ := filepath.Glob(filepath.Join(c.home, "cli-telemetry-ping-*")); len(matches) != 0 {
		t.Fatalf("an aborted ping left a claim: %v", matches)
	}
}

func TestShutdownEndsTheLoopWhileWaitingForTomorrow(t *testing.T) {
	clearPolicyEnv(t)
	c := testClient(testenv.TempDir(t), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return telemetryResponse(http.StatusAccepted), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.background(ctx, true); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background kept waiting after its context ended")
	}
}

func TestPingBodyIsByteIdenticalToTheLaunchPing(t *testing.T) {
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	ft.stop = func() bool { return true }
	c.version = "v2.30.0"
	c.background(ctx, true)
	want := `{"installId":"` + strings.Repeat("a", 32) + `","version":"v2.30.0","os":"` + runtime.GOOS +
		`","arch":"` + runtime.GOARCH + `","surface":"studio"}`
	if len(rec.bodies) != 1 || rec.bodies[0] != want {
		t.Fatalf("body = %v\nwant   %s", rec.bodies, want)
	}
}

type pingWire struct {
	mu     sync.Mutex
	bodies []string
	paths  []string
	fail   int
	hang   bool
}

func (w *pingWire) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	hang := w.hang
	fail := w.fail > 0
	if fail {
		w.fail--
	}
	if !hang && !fail {
		w.bodies = append(w.bodies, string(b))
	}
	w.paths = append(w.paths, r.URL.Path)
	w.mu.Unlock()
	if hang {
		<-r.Context().Done()
		return
	}
	if fail {
		rw.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	rw.WriteHeader(http.StatusAccepted)
}

func (w *pingWire) landed() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.bodies...)
}

var (
	wireOnce sync.Once
	wireMu   sync.Mutex
	wireNow  *pingWire
)

// One server for the package: the endpoint is a package variable that background
// goroutines read, so it is written once before any of them exists.
func serveWire(t *testing.T, w *pingWire) {
	t.Helper()
	wireOnce.Do(func() {
		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			wireMu.Lock()
			cur := wireNow
			wireMu.Unlock()
			cur.ServeHTTP(rw, r)
		}))
		endpoint, pingRetryBase = srv.URL+"/v1", 10*time.Millisecond
	})
	wireMu.Lock()
	wireNow = w
	wireMu.Unlock()
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for range 400 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// The boundary a person can observe: the request a launched Studio puts on the
// wire. A refusal on the first attempt must not cost the day's count, and the
// retry must carry the same bytes as the launch ping did.
func TestStartedReporterLandsOneIdenticalPingDespiteARefusal(t *testing.T) {
	clearPolicyEnv(t)
	wire := &pingWire{fail: 2}
	serveWire(t, wire)
	home := testenv.TempDir(t)
	ctx := t.Context()

	if Start(Options{Context: ctx, Mode: "on", Version: "v2.30.0", Surface: surface.Studio, HomeDir: home, Interactive: true}) == nil {
		t.Fatal("Start returned nil")
	}
	eventually(t, func() bool { return len(wire.landed()) == 1 })
	time.Sleep(100 * time.Millisecond)

	id, err := installID(home)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"installId":"` + id + `","version":"v2.30.0","os":"` + runtime.GOOS + `","arch":"` + runtime.GOARCH + `","surface":"studio"}`
	if got := wire.landed(); len(got) != 1 || got[0] != want {
		t.Fatalf("landed = %v\nwant     %s", got, want)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if len(wire.paths) != 3 {
		t.Fatalf("requests = %v, want two refusals and one success", wire.paths)
	}
}

func TestStartNeverWaitsOnTheNetworkAndStopsWithItsContext(t *testing.T) {
	clearPolicyEnv(t)
	wire := &pingWire{hang: true}
	serveWire(t, wire)
	ctx, cancel := context.WithCancel(context.Background())
	began := time.Now()
	Start(Options{Context: ctx, Mode: "on", Version: "v2.30.0", Surface: surface.Studio, HomeDir: testenv.TempDir(t), Interactive: true})
	if took := time.Since(began); took > 200*time.Millisecond {
		t.Fatalf("Start blocked for %v on a hung endpoint", took)
	}
	eventually(t, func() bool { wire.mu.Lock(); defer wire.mu.Unlock(); return len(wire.paths) == 1 })
	cancel()
	time.Sleep(200 * time.Millisecond)
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if len(wire.paths) != 1 {
		t.Fatalf("requests after shutdown = %v", wire.paths)
	}
}

func TestOptOutsSendNoPingAtAll(t *testing.T) {
	for name, set := range map[string]func(*testing.T) Options{
		"mode off": func(*testing.T) Options { return Options{Mode: "off"} },
		"do not track": func(t *testing.T) Options {
			t.Setenv("DO_NOT_TRACK", "1")
			return Options{Mode: "on"}
		},
		"ci": func(t *testing.T) Options {
			t.Setenv("CI", "true")
			return Options{Mode: "on"}
		},
		"dev build":  func(*testing.T) Options { return Options{Mode: "on", Version: "dev"} },
		"suppressed": func(*testing.T) Options { return Options{Mode: "on", SuppressPing: true} },
		"withdrawn":  func(*testing.T) Options { return Options{Mode: "on", PingAllowed: func() bool { return false }} },
	} {
		t.Run(name, func(t *testing.T) {
			clearPolicyEnv(t)
			wire := &pingWire{}
			serveWire(t, wire)
			opts := set(t)
			opts.Context, opts.Surface, opts.HomeDir, opts.Interactive = t.Context(), surface.Studio, testenv.TempDir(t), true
			if opts.Version == "" {
				opts.Version = "v2.30.0"
			}
			Start(opts)
			time.Sleep(150 * time.Millisecond)
			wire.mu.Lock()
			defer wire.mu.Unlock()
			if len(wire.paths) != 0 {
				t.Fatalf("requests = %v", wire.paths)
			}
		})
	}
}

func TestConsentWithdrawnWhileRetryingSendsNothingFurther(t *testing.T) {
	rec := &pingRecorder{fail: 100}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	on := true
	c.allowed = func() bool { return on }
	attempts := 0
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		on = false
		return nil, errors.New("offline")
	})}
	n := 0
	ft.stop = func() bool { n++; return n == 6 }
	c.background(ctx, true)
	if attempts != 1 {
		t.Fatalf("attempts = %d, want the one made before consent was withdrawn", attempts)
	}
	for _, d := range ft.sleeps[1:] {
		if d != maxWait {
			t.Fatalf("idle wait = %v, want %v: %v", d, maxWait, ft.sleeps)
		}
	}
}

func TestConsentWithdrawnWhileWaitingForTomorrowSendsNothing(t *testing.T) {
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	on := true
	c.allowed = func() bool { return on }
	ft.stop = func() bool { return ft.t.After(day1.Add(40 * time.Hour)) }
	slept := 0
	base := ft.sleep
	c.sleep = func(ctx context.Context, d time.Duration) bool {
		slept++
		if slept == 2 {
			on = false
		}
		return base(ctx, d)
	}
	c.background(ctx, true)
	if rec.count() != 1 {
		t.Fatalf("pings = %d, want only today's", rec.count())
	}
}

func TestConsentRestoredResumesPinging(t *testing.T) {
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	on := false
	c.allowed = func() bool { return on }
	slept := 0
	base := ft.sleep
	c.sleep = func(ctx context.Context, d time.Duration) bool {
		slept++
		if slept == 2 {
			on = true
		}
		return base(ctx, d)
	}
	ft.stop = func() bool { return rec.count() == 1 }
	c.background(ctx, true)
	if rec.count() != 1 {
		t.Fatalf("pings = %d", rec.count())
	}
}

func TestEnvironmentOptOutWhileRunningStopsThePing(t *testing.T) {
	clearPolicyEnv(t)
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	t.Setenv("DO_NOT_TRACK", "1")
	ft.stop = func() bool { return len(ft.sleeps) >= 3 }
	c.background(ctx, true)
	if rec.count() != 0 {
		t.Fatalf("pings = %d under DO_NOT_TRACK", rec.count())
	}
}

func TestRefusalClassesDecideWhetherTheDayIsRetried(t *testing.T) {
	cases := []struct {
		code      int
		permanent bool
	}{
		{400, true}, {401, true}, {403, true}, {404, true}, {410, true},
		{429, false}, {500, false}, {502, false}, {503, false},
	}
	for _, tc := range cases {
		rec := &pingRecorder{}
		c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
		attempts := 0
		c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return telemetryResponse(tc.code), nil
		})}
		ft.stop = func() bool { return ft.t.After(day1.Add(11 * time.Hour)) }
		c.background(ctx, true)
		if tc.permanent && attempts != 1 {
			t.Errorf("HTTP %d attempts = %d, want 1 for the rest of the day", tc.code, attempts)
		}
		if !tc.permanent && attempts < 4 {
			t.Errorf("HTTP %d attempts = %d, want backoff retries", tc.code, attempts)
		}
	}
}

func TestPermanentRefusalIsTriedAgainOnTheNextUTCDay(t *testing.T) {
	rec := &pingRecorder{}
	c, ft, ctx := loopClient(t, testenv.TempDir(t), rec, day1)
	attempts := 0
	c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return telemetryResponse(http.StatusForbidden), nil
	})}
	ft.stop = func() bool { return attempts == 2 }
	c.background(ctx, true)
	if ft.t.Before(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("second attempt at %v, before the next UTC day", ft.t)
	}
}

func TestStatusErrorCarriesItsCodeAsAType(t *testing.T) {
	err := error(&statusError{code: 404})
	var se *statusError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &se) || !se.permanent() {
		t.Fatal("404 not recognised through wrapping")
	}
}
