package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
)

// fakeServe is a minimal Serve stand-in for bridge tests: token handshake,
// session enter, an SSE feed that emits two frames then holds, and recorded
// command endpoints for the proxy bindings.
type fakeServe struct {
	t      *testing.T
	token  string
	server *httptest.Server

	mu                             sync.Mutex
	newCalled                      int
	resumePath                     string
	cookieOnNew                    bool
	sessions                       []serveSessionEntry
	calls                          []string // "METHOD /path body" per command request
	failNext                       string   // non-empty ⇒ next command endpoint replies 409 with this text
	failEnter                      string   // non-empty ⇒ next /new or /resume replies 409
	enterDelay                     time.Duration
	failHistory                    bool // /history replies 500 when set
	historyStarted, historyRelease chan struct{}
	failSessions                   bool // /sessions replies 500 when set
	sessionsStarted                chan struct{}
	sessionsRelease                chan struct{}
	eventsConns                    int  // /events connections opened
	eventsStatus                   int  // non-zero makes /events fail before opening
	eventsCloseEarly               bool // return immediately after the initial 200 frames
	statusPayload                  string
	statusAfterCancel              string
}

func (fs *fakeServe) eventsCount() int { fs.mu.Lock(); defer fs.mu.Unlock(); return fs.eventsConns }

func (fs *fakeServe) recorded() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]string, len(fs.calls))
	copy(out, fs.calls)
	return out
}

func (fs *fakeServe) record(method, path, body string) {
	fs.mu.Lock()
	fs.calls = append(fs.calls, method+" "+path+" "+body)
	fs.mu.Unlock()
}

// newFakeServe builds a stand-in for the workspace Serve's HTTP surface. The
// mux is wrapped in a token-mode gate that mirrors the real authGate's
// contract: POST /auth/token is matched on the EXACT path (a "//auth/token"
// double slash — what naive base+path joins produce from EnsureServer's
// trailing-slash LocalURL — is denied with 401 before routing), and every
// other path requires the session cookie the bootstrap installs. A bare mux
// cannot catch this: it 301-redirects unclean paths and Go's client follows
// preserving POST, so a double-slash request would silently succeed here
// while the real Serve rejects it.
func newFakeServe(t *testing.T, token string, sessions []serveSessionEntry) *fakeServe {
	t.Helper()
	fs := &fakeServe{t: t, token: token, sessions: sessions}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token != fs.token {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "reasonix_token", Value: fs.token, Path: "/", HttpOnly: true})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /new", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.newCalled++
		_, cookieErr := r.Cookie("reasonix_token")
		fs.cookieOnNew = cookieErr == nil
		fail := fs.failEnter
		fs.failEnter = ""
		enterDelay := fs.enterDelay
		if fail != "" {
			fs.mu.Unlock()
			http.Error(w, fail, http.StatusConflict)
			return
		}
		// The serve abandons the current session on /new: no file, not listed.
		for i := range fs.sessions {
			fs.sessions[i].Current = false
		}
		fs.mu.Unlock()
		if enterDelay > 0 {
			time.Sleep(enterDelay)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /resume", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			http.Error(w, "missing path", http.StatusBadRequest)
			return
		}
		fs.mu.Lock()
		fail := fs.failEnter
		fs.failEnter = ""
		enterDelay := fs.enterDelay
		if fail != "" {
			fs.mu.Unlock()
			http.Error(w, fail, http.StatusConflict)
			return
		}
		fs.resumePath = body.Path
		for i := range fs.sessions {
			fs.sessions[i].Current = fs.sessions[i].Path == body.Path
		}
		fs.mu.Unlock()
		if enterDelay > 0 {
			time.Sleep(enterDelay)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r.Method, "/sessions", "")
		fs.mu.Lock()
		fail := fs.failSessions
		started, release := fs.sessionsStarted, fs.sessionsRelease
		fs.mu.Unlock()
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if release != nil {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if fail {
			http.Error(w, "sessions unavailable", http.StatusInternalServerError)
			return
		}
		writeTestJSON(w, fs.sessions)
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.eventsConns++
		eventsStatus := fs.eventsStatus
		closeEarly := fs.eventsCloseEarly
		fs.mu.Unlock()
		if eventsStatus != 0 {
			http.Error(w, "event stream unavailable", eventsStatus)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"kind":"session_start"}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"kind":"ready"}`)
		flusher.Flush()
		if closeEarly {
			return
		}
		<-r.Context().Done()
	})
	command := func(path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			data, _ := io.ReadAll(io.LimitReader(r.Body, 4<<10))
			fs.record(r.Method, path, string(data))
			fs.mu.Lock()
			fail := fs.failNext
			fs.failNext = ""
			if path == "/cancel" && fs.statusAfterCancel != "" {
				fs.statusPayload = fs.statusAfterCancel
			}
			fs.mu.Unlock()
			if fail != "" {
				http.Error(w, fail, http.StatusConflict)
				return
			}
			if path == "/composer-profile" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"drainedApprovalIDs":["approval-1"]}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	for _, path := range []string{"/submit", "/cancel", "/approve", "/plan-decision", "/answer", "/extension-form", "/rewind", "/goal", "/goal/pause", "/goal/resume", "/jobs/cancel", "/inbox/items", "/tool-approval-mode", "/composer-profile", "/delete-session", "/model", "/effort", "/quality-floor", "/plan", "/compact", "/fork", "/summarize", "/forget", "/clear"} {
		mux.HandleFunc("POST "+path, command(path))
	}
	snapshot := func(path, payload string) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			fs.record(r.Method, path, "")
			if path == "/history" {
				fs.mu.Lock()
				fail := fs.failHistory
				started, release := fs.historyStarted, fs.historyRelease
				fs.mu.Unlock()
				if started != nil {
					started <- struct{}{}
				}
				if release != nil {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if fail {
					http.Error(w, "gone", http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		})
	}
	snapshot("/history", `[{"role":"user","content":"hi"}]`)
	snapshot("/context", `{"used":10}`)
	snapshot("/todos", `[]`)
	snapshot("/checkpoints", `[{"turn":1}]`)
	snapshot("/models", `{"current":"remote/chat","label":"chat","models":[{"ref":"remote/chat","provider":"remote","model":"chat","active":true}]}`)
	snapshot("/commands", `[{"name":"remote-review","description":"Review remotely","kind":"custom","group":"skills"}]`)
	snapshot("/pending-prompts", `[{"kind":"approval_request","approval":{"id":"approval-1","tool":"bash"}}]`)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		fs.record(r.Method, "/status", "")
		fs.mu.Lock()
		payload := fs.statusPayload
		fs.mu.Unlock()
		if payload == "" {
			payload = `{"state":"ready"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	})
	snapshot("/branches", `{"branches":[]}`)
	snapshot("/skills", `[]`)
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/token" {
			mux.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("reasonix_token"); err == nil && c.Value == fs.token {
			mux.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	fs.server = httptest.NewServer(gate)
	t.Cleanup(fs.server.Close)
	return fs
}

func (fs *fakeServe) snapshot() (newCalled int, resumePath string, cookieOnNew bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.newCalled, fs.resumePath, fs.cookieOnNew
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func seedBridgeTestHost(t *testing.T, hostID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: hostID, Host: "127.0.0.1", Port: 22, User: "dev"})
	}); err != nil {
		t.Fatal(err)
	}
}

// eventLog records every emitRemoteEvent call from any goroutine.
type eventLog struct {
	mu     sync.Mutex
	events []string // "name payload"
}

func (l *eventLog) add(name string, payload any) {
	text, _ := json.Marshal(payload)
	l.mu.Lock()
	l.events = append(l.events, name+" "+string(text))
	l.mu.Unlock()
}

func (l *eventLog) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	copy(out, l.events)
	return out
}

func (l *eventLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func waitForTabState(t *testing.T, a *App, tabID, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.remoteTabMu.Lock()
		tab := a.remoteTabs[tabID]
		state := ""
		if tab != nil {
			state = tab.state
		}
		a.remoteTabMu.Unlock()
		if state == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("remote tab %s state = %q, want %q", tabID, state, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// cleanupRemoteTabPumps cancels every open tab's SSE pump when the test
// ends. Without this the long-lived /events connection keeps the httptest
// server's Close waiting forever.
func cleanupRemoteTabPumps(t *testing.T, a *App) {
	t.Helper()
	t.Cleanup(func() {
		a.remoteTabMu.Lock()
		for _, tab := range a.remoteTabs {
			if tab.cancel != nil {
				tab.cancel()
			}
		}
		a.remoteTabMu.Unlock()
	})
}

// TestRemoteTabBridgeEntersNewSessionAndStreams pins the happy path: open →
// handshake → pump subscribed → POST /new → ready, with frames forwarded on
// the tab's event channel and the session cookie riding the jar.
func TestRemoteTabBridgeEntersNewSessionAndStreams(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	newCalled, _, cookieOnNew := fs.snapshot()
	if newCalled != 1 {
		t.Fatalf("POST /new called %d times, want 1", newCalled)
	}
	if !cookieOnNew {
		t.Fatal("POST /new carried no session cookie; handshake did not populate the jar")
	}
	if got := log.count("remote-tab:" + meta.ID + ":event"); got < 2 {
		t.Fatalf("pump forwarded %d frames, want ≥2 (events: %v)", got, log.events)
	}
	if log.count("remote-tab:"+meta.ID+":state") < 2 {
		t.Fatalf("expected connecting + ready state events, got %v", log.events)
	}

	// Cancelling the pump (close/reconnect) must exit silently: no error
	// state is emitted for a deliberate stop.
	a.remoteTabMu.Lock()
	cancel := a.remoteTabs[meta.ID].cancel
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
	time.Sleep(100 * time.Millisecond)
	a.remoteTabMu.Lock()
	state := a.remoteTabs[meta.ID].state
	a.remoteTabMu.Unlock()
	if state != "ready" {
		t.Fatalf("state after pump cancel = %q, want ready (silent exit)", state)
	}
}

func TestConcurrentRemoteProjectOpenReusesOneTab(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	start := make(chan struct{})
	results := make(chan TabMeta, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
			results <- meta
			errs <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("concurrent opens returned %q and %q", first.ID, second.ID)
	}
	a.remoteTabMu.Lock()
	count := len(a.remoteTabs)
	a.remoteTabMu.Unlock()
	if count != 1 {
		t.Fatalf("remote tab count = %d, want one", count)
	}
}

// TestRemoteTabBridgeToleratesTrailingSlashBase pins the production LocalURL
// shape: EnsureServer reports "http://127.0.0.1:port/" with a trailing slash.
// Naive base+"/auth/token" concatenation used to hit "//auth/token", which the
// serve auth gate rejects with 401 before routing the endpoint — the handshake
// died on every wizard-completed connect.
func TestRemoteTabBridgeToleratesTrailingSlashBase(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL + "/"},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	newCalled, _, cookieOnNew := fs.snapshot()
	if newCalled != 1 || !cookieOnNew {
		t.Fatalf("handshake with trailing-slash base failed: /new=%d cookie=%v", newCalled, cookieOnNew)
	}
}

func TestRemoteTabBusyAttachKeepsCurrentSessionMetadata(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "current", Path: "/sessions/current.jsonl", Title: "Current work", Current: true},
	})
	fs.mu.Lock()
	fs.failEnter = "cannot start a new session while a turn is running"
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	reset, title := tab.session.reset, tab.topicTitle
	a.remoteTabMu.Unlock()
	if reset {
		t.Fatal("a refused /new must not mark the current session as a blank reset")
	}
	if title == a.localizedDefaultTopicTitle() {
		t.Fatalf("a refused /new replaced the current title with %q", title)
	}
	if err := a.SubmitRemoteTab(meta.ID, "continue"); err != nil {
		t.Fatalf("busy attach did not keep the current session usable: %v", err)
	}
}

// TestRemoteTabBridgeHandshakeFailureSurfacesError pins that a rejected
// token lands the tab in error instead of a phantom ready shell.
func TestRemoteTabBridgeHandshakeFailureSurfacesError(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "wrong-token",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "error")
	a.remoteTabMu.Lock()
	tabErr := a.remoteTabs[meta.ID].err
	a.remoteTabMu.Unlock()
	if !strings.Contains(tabErr, "401") {
		t.Fatalf("tab error = %q, want handshake 401", tabErr)
	}
	if _, _, cookieOnNew := fs.snapshot(); cookieOnNew {
		t.Fatal("POST /new must not be reached after a failed handshake")
	}
}

// TestRemoteTabBridgeResumeResolvesSessionPath pins the name→path
// resolution: a SessionName open reads GET /sessions and POSTs /resume with
// the entry's path, never the bare name.
func TestRemoteTabBridgeResumeResolvesSessionPath(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First", Turns: 2},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")

	newCalled, resumePath, _ := fs.snapshot()
	if newCalled != 0 {
		t.Fatalf("POST /new called %d times, want 0 for a resume open", newCalled)
	}
	if resumePath != "/remote/sessions/s1.jsonl" {
		t.Fatalf("POST /resume path = %q, want the /sessions entry path", resumePath)
	}
}

// TestEnterRemoteSessionUnknownName fails fast on an unlisted session name.
func TestEnterRemoteSessionUnknownName(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "s1", Path: "/x.jsonl"}})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := serveHandshake(ctx, client, fs.server.URL, "s3cret"); err != nil {
		t.Fatal(err)
	}
	err = enterRemoteSession(ctx, client, fs.server.URL, RemoteTabOpenOptions{SessionName: "missing"})
	if err == nil || !strings.Contains(err.Error(), `"missing" not found`) {
		t.Fatalf("err = %v, want unknown session error", err)
	}
}

// openReadyRemoteTab opens a tab against the fake serve and waits for ready.
func openReadyRemoteTab(t *testing.T, a *App, opts RemoteTabOpenOptions) TabMeta {
	t.Helper()
	meta, err := a.OpenRemoteProjectTab("box", "~/app", opts)
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	return meta
}

// TestRemoteTabCommandRejectsUnknownOrUnreadyTab: an unknown tabID and a
// tab that has not finished bootstrap are errors, never silent no-ops.
func TestRemoteTabCommandRejectsUnknownOrUnreadyTab(t *testing.T) {
	a := &App{}
	if err := a.SubmitRemoteTab("missing", "hi"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unknown tab err = %v, want not connected", err)
	}
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{"booting": {id: "booting", state: "connecting"}}
	a.remoteTabMu.Unlock()
	if err := a.SubmitRemoteTab("booting", "hi"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unready tab err = %v, want not connected", err)
	}
}

// TestModelsForTabRemoteUsesDesktopCatalog: a remote tab's model switcher
// lists the desktop provider catalog. Current is the desktop-owned tab model,
// not the remote serve GET /models payload.
func TestModelsForTabRemoteUsesDesktopCatalog(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	})
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev", CredentialMode: "local-proxy"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	got := a.ModelsForTab(meta.ID)
	refs := map[string]bool{}
	current := ""
	for _, m := range got {
		refs[m.Ref] = true
		if m.Current {
			current = m.Ref
		}
	}
	if !refs["deepseek/deepseek-v4-flash"] || !refs["deepseek/deepseek-v4-pro"] {
		t.Fatalf("ModelsForTab(%s) = %+v, want desktop deepseek catalog", meta.ID, got)
	}
	if refs["remote/chat"] {
		t.Fatalf("ModelsForTab leaked the serve catalog: %+v", got)
	}
	if current != "deepseek/deepseek-v4-flash" {
		t.Fatalf("current = %q, want desktop default_model", current)
	}
}

// TestSetModelForTabRemoteOwnsModelOnDesktop: picking a model on a remote tab
// delegates the failure-atomic switch to the kernel before committing desktop
// tab metadata; the binding itself must not issue a second Serve request.
func TestSetModelForTabRemoteOwnsModelOnDesktop(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	})
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev", CredentialMode: "local-proxy"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	if err := a.SetModelForTab(meta.ID, "deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	if len(kernel.switchProxyCalls) != 1 || kernel.switchProxyCalls[0] != [4]string{"box", "~/app", "deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-pro"} {
		t.Fatalf("credential proxy switch calls = %+v", kernel.switchProxyCalls)
	}
	for _, c := range fs.recorded() {
		if strings.HasPrefix(c, "POST /model") {
			t.Fatalf("serve saw %v, binding duplicated the kernel-owned switch", fs.recorded())
		}
	}
	var current string
	for _, m := range a.ModelsForTab(meta.ID) {
		if m.Current {
			current = m.Ref
		}
	}
	if current != "deepseek/deepseek-v4-pro" {
		t.Fatalf("current = %q, want deepseek/deepseek-v4-pro", current)
	}
}

// TestSetModelForTabRemoteCredentialPostsServeModel: remote-credential hosts
// switch through the serve's per-session endpoint.
func TestSetModelForTabRemoteCredentialPostsServeModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	a.remoteTabLayout.activeID = "other-tab"
	a.remoteTabMu.Unlock()

	if err := a.SetModelForTab(meta.ID, "remote/chat"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	posted := false
	for _, c := range fs.recorded() {
		if strings.HasPrefix(c, "POST /model ") && strings.Contains(c, `"ref":"remote/chat"`) {
			posted = true
		}
	}
	if !posted {
		t.Fatalf("serve never saw POST /model with the ref: %v", fs.recorded())
	}
	a.remoteTabMu.Lock()
	model, activeID := "", a.remoteTabLayout.activeID
	if tab := a.remoteTabs[meta.ID]; tab != nil {
		model = tab.model
	}
	a.remoteTabMu.Unlock()
	if model != "remote/chat" {
		t.Fatalf("tab.model = %q, want remote/chat", model)
	}
	if activeID != "other-tab" {
		t.Fatalf("completed model switch reactivated %q, want other-tab to stay active", activeID)
	}
	if !slices.ContainsFunc(log.recorded(), func(event string) bool { return strings.HasPrefix(event, "remote-tab:updated ") }) {
		t.Fatalf("model switch did not publish metadata update: %v", log.recorded())
	}
}

// TestSetRemoteTabModelFailureKeepsPreviousModel: a local-proxy switch that
// fails at the credential-proxy step must leave the tab's previous model
// intact instead of half-committing the new one.
