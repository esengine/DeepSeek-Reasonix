package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/frontend/traystate"
	"reasonix/internal/session/control"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}

func TestStudioPingConsentIsReadFreshEachTime(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opts := studioTelemetryOptions(context.Background(), config.Default(), "2.20.2")
	if opts.PingAllowed == nil {
		t.Fatal("Studio's ping has no live consent check")
	}
	write("[desktop]\ntelemetry = true\n")
	if !opts.PingAllowed() {
		t.Fatal("enabled setting reads as off")
	}
	write("[desktop]\ntelemetry = false\n")
	if opts.PingAllowed() {
		t.Fatal("a setting turned off while running still allows the ping")
	}
	write("[desktop]\ntelemetry = false\n\n[broken\nx = \n")
	if opts.PingAllowed() {
		t.Fatal("a corrupt file after an opt-out counted as consent")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !opts.PingAllowed() {
		t.Fatal("a missing file should keep the documented default (on)")
	}
}

func TestProjectConfigCannotTurnThePingOn(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[desktop]\ntelemetry = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[desktop]\ntelemetry = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	if desktopTelemetryOn() {
		t.Fatal("a checkout's reasonix.toml turned the ping back on")
	}
}

func TestStudioTelemetryUsesItsOwnSurfaceAndConsent(t *testing.T) {
	cfg := config.Default()
	disabled := false
	cfg.Desktop.Telemetry = &disabled

	opts := studioTelemetryOptions(context.Background(), cfg, "2.20.2")
	if opts.Surface != surface.Studio {
		t.Fatalf("surface = %q, want studio", opts.Surface)
	}
	if !opts.SuppressPing {
		t.Fatal("disabled launch telemetry did not suppress the Studio ping")
	}
	if opts.Version != "2.20.2" || opts.Mode != "on" || !opts.Interactive {
		t.Fatalf("Studio telemetry options = %+v", opts)
	}
}

// recordingRunner stands in for the agent: a turn that reaches it proves the
// request crossed the boundary and landed in the kernel, which is the only
// thing a status code cannot say.
type recordingRunner struct{ got chan string }

func (r recordingRunner) Run(_ context.Context, input string) error {
	r.got <- input
	return nil
}

// testHub is the production assembly with one piece swapped: the runner. The
// hub, its config path and its capabilities are the ones the host builds.
func testHub(t *testing.T, cfg config.ServeConfig) (*serve.Hub, chan string) {
	t.Helper()
	root := t.TempDir()
	got := make(chan string, 1)
	bc := serve.NewBroadcaster()
	ctrl := control.New(control.Options{
		Runner:        recordingRunner{got: got},
		Sink:          bc,
		WorkspaceRoot: root,
		SessionDir:    serve.SessionDirFor(root),
	})
	t.Cleanup(ctrl.Close)
	hubCfg := hostServeConfig(cfg)
	// A tray, because the tray routes exist only where there is one. Without it
	// those paths were never registered, and the boundary test that reads them
	// was answered by whatever the kernel served for an unknown path.
	hub := serve.NewHub(serve.HubOptions{Serve: hubCfg, Surface: surface.Desktop, Grant: grantHostCapabilities, Tray: stubTray{}})
	srv := serve.New(ctrl, bc, hubCfg)
	srv.SetPaneSink(bc)
	if _, err := hub.Adopt(srv, bc); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	t.Cleanup(hub.Shutdown)
	return hub, got
}

// startHost binds and serves the way run does, and reports when the socket has
// finished draining so a test can assert on what is left behind.
func startHost(t *testing.T, cfg config.ServeConfig) (*bound, chan string, func()) {
	t.Helper()
	hub, got := testHub(t, cfg)
	b, err := bind(hub.Handler())
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.serve(ctx) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve returned %v, want a clean drain", err)
		}
	}
	t.Cleanup(stop)
	return b, got, stop
}

// ask sends a request the host's own page would send, unless a test overrides
// one of the pieces the boundary reads.
func ask(t *testing.T, b *bound, method, path string, mut func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, b.origin+path, strings.NewReader(`{"input":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", b.origin)
	req.AddCookie(&http.Cookie{Name: serve.TokenCookie, Value: b.token})
	if mut != nil {
		mut(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

// The socket is never reachable from anywhere but this machine, and the address
// is what the origin and every later check are derived from.
func TestHostBindsOnlyToLoopback(t *testing.T) {
	b, err := bind(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer b.listener.Close()

	host, port, err := net.SplitHostPort(b.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Errorf("listening on %q, want 127.0.0.1", host)
	}
	if want := "http://" + net.JoinHostPort(host, port); b.origin != want {
		t.Errorf("origin = %q, want %q", b.origin, want)
	}
}

// The credential belongs to one launch. Two hosts started from the same machine
// and the same config must not be able to drive each other.
func TestHostMintsAFreshCredentialPerLaunch(t *testing.T) {
	first, err := bind(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer first.listener.Close()
	second, err := bind(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer second.listener.Close()

	if first.token == second.token {
		t.Fatal("two launches minted the same credential")
	}
	if len(first.token) != credentialBytes*2 {
		t.Errorf("credential is %d hex chars, want %d", len(first.token), credentialBytes*2)
	}
}

// Studio's boundary is not the user's serve policy. A configuration that turns
// authentication off for `reasonix serve` does not turn it off here.
func TestHostRequiresItsCredentialUnderAuthModeNone(t *testing.T) {
	b, got, _ := startHost(t, config.ServeConfig{AuthMode: "none"})

	status, body := ask(t, b, http.MethodPost, "/submit", func(r *http.Request) {
		r.Header.Del("Cookie")
	})
	if status != http.StatusForbidden || !strings.Contains(body, "loopback.unauthorized") {
		t.Fatalf("status = %d %s, want 403 loopback.unauthorized", status, body)
	}
	select {
	case in := <-got:
		t.Fatalf("an unauthenticated request reached the kernel with %q", in)
	default:
	}
}

// A token the user configured for `reasonix serve` is not this launch's
// credential, and it does not become one by being in the config file.
func TestHostDoesNotAdoptTheConfiguredServeToken(t *testing.T) {
	const configured = "a-token-the-user-put-in-their-config"
	b, got, _ := startHost(t, config.ServeConfig{AuthMode: "token", Token: configured})

	if b.token == configured {
		t.Fatal("the host adopted the configured serve token as its own credential")
	}
	status, body := ask(t, b, http.MethodPost, "/submit", func(r *http.Request) {
		r.Header.Del("Cookie")
		r.AddCookie(&http.Cookie{Name: serve.TokenCookie, Value: configured})
	})
	if status != http.StatusForbidden || !strings.Contains(body, "loopback.unauthorized") {
		t.Fatalf("status = %d %s, want 403 loopback.unauthorized", status, body)
	}
	select {
	case in := <-got:
		t.Fatalf("the configured token drove the kernel with %q", in)
	default:
	}

	// And the launch credential still works, which is what proves the two gates
	// are not both trying to own the one cookie.
	if status, body := ask(t, b, http.MethodPost, "/submit", nil); status >= http.StatusBadRequest {
		t.Fatalf("status = %d %s, want the turn accepted", status, body)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the kernel")
	}
}

// Reads and writes both have to arrive, or the boundary is only proving it can
// refuse things.
func TestHostPassesRealTrafficToTheKernel(t *testing.T) {
	b, got, _ := startHost(t, config.ServeConfig{})

	status, body := ask(t, b, http.MethodGet, "/status", func(r *http.Request) {
		r.Header.Del("Origin")
	})
	if status != http.StatusOK {
		t.Fatalf("GET /status = %d %s, want 200", status, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("GET /status did not answer with the kernel's JSON: %v", err)
	}

	if status, body := ask(t, b, http.MethodPost, "/submit", nil); status >= http.StatusBadRequest {
		t.Fatalf("POST /submit = %d %s", status, body)
	}
	select {
	case in := <-got:
		if in != "hello" {
			t.Errorf("the kernel ran %q, want %q", in, "hello")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the kernel")
	}
}

// The stream still streams. A gate that buffered would leave every frame
// waiting for the turn to end, which is the failure Wails' asset server had.
func TestHostStreamsEventsThroughTheGate(t *testing.T) {
	b, _, _ := startHost(t, config.ServeConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.origin+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: serve.TokenCookie, Value: b.token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/events = %d, want 200", resp.StatusCode)
	}
	// The opening comment arrives before any event does, and only because the
	// handler flushed it — nothing else has been written yet.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("the stream delivered nothing: %v", err)
	}
	if !strings.HasPrefix(line, ":") {
		t.Fatalf("first line = %q, want the stream's opening comment", line)
	}
}

// Draining has to actually release the socket: a host that returned while the
// port was still bound would refuse the next launch its own listener.
func TestHostReleasesTheSocketOnShutdown(t *testing.T) {
	b, _, stop := startHost(t, config.ServeConfig{})
	addr := b.listener.Addr().String()

	if status, body := ask(t, b, http.MethodGet, "/status", nil); status != http.StatusOK {
		t.Fatalf("GET /status = %d %s, want 200 before shutdown", status, body)
	}
	stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("%s still accepts connections after shutdown", addr)
	}
}

// A parent holds a lease by holding a pipe. Nothing else is one: a terminal
// would never end, and /dev/null would end at once and take the host with it.
func TestOnlyAPipeIsAParentLease(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if parentLease(r) == nil {
		t.Error("a pipe is a lease and was not read as one")
	}

	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if parentLease(null) != nil {
		t.Error("the null device was read as a parent holding this host open")
	}

	path := filepath.Join(t.TempDir(), "not-a-pipe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if parentLease(file) != nil {
		t.Error("a regular file was read as a parent holding this host open")
	}
}

// The whole run, driven the way Electron drives it: a pipe for the lease, one
// line of handshake, traffic, then the parent letting go.
func TestRunServesUntilTheParentLetsGo(t *testing.T) {
	page := t.TempDir()
	if err := os.WriteFile(filepath.Join(page, "index.html"), []byte("<title>studio</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease, holder, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, announced, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	logs, logged, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, logs) }()

	done := make(chan int, 1)
	go func() { done <- run(lease, announced, logged, page, shellIdentity{version: "2.10.0"}) }()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("no handshake: %v", err)
	}
	var said handshake
	if err := json.Unmarshal([]byte(line), &said); err != nil {
		t.Fatalf("the handshake is not one JSON line: %v", err)
	}
	if said.Version != handshakeVersion {
		t.Errorf("handshake version = %d, want %d", said.Version, handshakeVersion)
	}
	if said.Origin == "" || said.Token == "" {
		t.Fatalf("the handshake said %+v", said)
	}

	req, err := http.NewRequest(http.MethodGet, said.Origin+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: serve.TokenCookie, Value: said.Token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>studio</title>") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, body)
	}

	// The parent goes away. Nothing is signalled: the pipe closing is the whole
	// message, which is what makes it identical on every platform.
	holder.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run returned %d, want a clean exit", code)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the host outlived the parent that was holding it open")
	}
	addr := strings.TrimPrefix(said.Origin, "http://")
	if conn, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		conn.Close()
		t.Errorf("%s still accepts connections after the lease ended", addr)
	}
}

// Notifications are the shared [notifications] setting, not a shell of its own:
// a window that announced turns the CLI would have kept quiet about would be a
// second policy nobody set. The switch is read from the holder on every event,
// so every runtime is wrapped whatever the setting says today — deciding it at
// boot is what made "notify me" mean "notify me after a restart".
func TestHostNotificationsCarryTheSharedSettingLive(t *testing.T) {
	plain := serve.NewBroadcaster()
	for _, cfg := range []*config.Config{
		{},
		{Notifications: config.NotificationsConfig{Enabled: true}},
		nil,
	} {
		wrap, set := hostNotifications(cfg)
		if got := wrap(plain); got == event.Sink(plain) {
			t.Fatalf("runtime was left unwrapped, so the switch could never reach it (cfg %+v)", cfg)
		}
		want := config.NotificationsConfig{}
		if cfg != nil {
			want = cfg.Notifications
		}
		if got := set.Load(); got != want {
			t.Errorf("holder = %+v, want the shared setting %+v", got, want)
		}
	}
}

// The tray surface is the control plane like any other: it decides what the
// close button does and whether an icon exists, and a page that guessed the
// port must not reach it just because the user's serve config has no password.
func TestTrayEndpointsSitBehindTheSameBoundary(t *testing.T) {
	b, _, _ := startHost(t, config.ServeConfig{AuthMode: "none"})

	for _, path := range []string{"/tray/prefs", "/tray/state"} {
		status, body := ask(t, b, http.MethodGet, path, func(r *http.Request) { r.Header.Del("Cookie") })
		if status != http.StatusForbidden || !strings.Contains(body, "loopback.unauthorized") {
			t.Errorf("GET %s = %d %s, want 403 loopback.unauthorized", path, status, body)
		}
	}
	// A write without this listener's origin is refused before it reaches any
	// config file.
	status, body := ask(t, b, http.MethodPut, "/tray/prefs", func(r *http.Request) { r.Header.Del("Origin") })
	if status != http.StatusForbidden || !strings.Contains(body, "loopback.origin_rejected") {
		t.Errorf("PUT /tray/prefs = %d %s, want 403 loopback.origin_rejected", status, body)
	}
	// And with both it reaches the kernel, which is what makes the refusals above
	// mean something.
	if status, body := ask(t, b, http.MethodGet, "/tray/prefs", nil); status != http.StatusOK {
		t.Errorf("GET /tray/prefs = %d %s, want 200", status, body)
	}
}

// stubTray is a window for the tray routes to answer about. It reports the
// quiet state a host reports before anything has run.
type stubTray struct{}

func (stubTray) IconLive() bool                 { return false }
func (stubTray) TrayFold() traystate.State      { return traystate.State{} }
func (stubTray) ApplyTrayPrefs(serve.TrayPrefs) {}

// A person who only ever opens the window still gets the one-time upgrades:
// the shipped "auto" a v6 file carries is released before the first pane is
// built, so that pane opens in the default the sandbox and trust decide.
func TestAssembleAppliesTheStartupUpgrades(t *testing.T) {
	// Its own home: another test's launch already recorded the release in the
	// package's shared one, and a recorded release is not run again.
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("config_version = 6\n[desktop]\ndefault_tool_approval_mode = \"auto\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(testenv.TempDir(t))
	hub, err := assemble(t.Context(), io.Discard, io.Discard, shellIdentity{}, nil, newStartupPhases(io.Discard, time.Now))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	defer hub.Shutdown()
	if got := config.LoadForEdit(path).DesktopDefaultToolApprovalMode(); got != "" {
		t.Fatalf("after the window started, the desktop posture is %q, want the shipped auto released", got)
	}
}
