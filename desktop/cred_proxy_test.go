package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/remote/bootstrap"
)

type failingRequestBody struct{}

func (failingRequestBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingRequestBody) Close() error             { return nil }

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(strings.TrimRight(raw, "/") + "/")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestCredentialProxyAuthSwap covers the desktop key holder over real HTTP:
// the registered virtual token forwards to the provider with the real key,
// anything else is rejected without reaching the provider.
func TestCredentialProxyAuthSwap(t *testing.T) {
	var gotAuth, gotForwarded string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Via"} {
			gotForwarded += r.Header.Get(h)
		}
		_, _ = w.Write([]byte("model-ok"))
	}))
	defer upstream.Close()

	seedBridgeTestHost(t, "box")
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	port, err := a.credentialProxyPort()
	if err != nil {
		t.Fatal(err)
	}
	const token = "virtual-tok"
	a.credProxy.setRoute(token, mustParseURL(t, upstream.URL), "sk-real-key", "", "")
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat", port)

	do := func(auth string) (int, string) {
		req, err := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("Connection", "Authorization")
		req.Header.Set("Forwarded", "for=attacker")
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, string(buf[:n])
	}

	if code, body := do("Bearer virtual-tok"); code != 200 || body != "model-ok" {
		t.Fatalf("valid token: code=%d body=%q", code, body)
	}
	if gotAuth != "Bearer sk-real-key" {
		t.Fatalf("upstream auth = %q, want the real key", gotAuth)
	}
	if gotForwarded != "" {
		t.Fatalf("forwarding identity leaked upstream: %q", gotForwarded)
	}
	if code, _ := do("Bearer wrong"); code != 401 {
		t.Fatalf("wrong token: code=%d, want 401", code)
	}
	if gotAuth != "Bearer sk-real-key" {
		t.Fatalf("rejected request reached the upstream: %q", gotAuth)
	}
	if code, _ := do(""); code != 401 {
		t.Fatalf("missing token: code=%d, want 401", code)
	}
}

// TestCredentialProxyRewritesRequestModel: desktop owns the current model, so
// the proxy replaces the serve's request-body model with the desktop selection
// before the real provider sees it. The provider must also see its OWN host
// in the Host header — the inbound loopback host must not leak through
// (CloudFront-fronted APIs 403 a foreign Host).
func TestCredentialProxyRewritesRequestModel(t *testing.T) {
	var gotBody, gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		gotHost = r.Host
		_, _ = w.Write([]byte("model-ok"))
	}))
	defer upstream.Close()

	seedBridgeTestHost(t, "box")
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	port, err := a.credentialProxyPort()
	if err != nil {
		t.Fatal(err)
	}
	const token = "virtual-tok"
	a.credProxy.setRoute(token, mustParseURL(t, upstream.URL), "sk-real-key", "deepseek-v4-pro", "openai")

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port), strings.NewReader(`{"model":"deepseek-v4-flash","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer virtual-tok")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(gotBody, `"model":"deepseek-v4-pro"`) {
		t.Fatalf("upstream body = %q, want rewritten model deepseek-v4-pro", gotBody)
	}
	if strings.Contains(gotBody, "deepseek-v4-flash") {
		t.Fatalf("upstream still saw the serve's model: %q", gotBody)
	}
	if want := strings.TrimPrefix(strings.TrimPrefix(upstream.URL, "http://"), "http://"); gotHost != want {
		t.Fatalf("upstream Host = %q, want the upstream's own host %q", gotHost, want)
	}
}

func TestCredentialProxyRejectsUnreadableOrOversizeBodies(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls++
	}))
	defer upstream.Close()
	p := &credentialProxy{routes: map[string]*credProxyRoute{}}
	p.setRoute("virtual-tok", mustParseURL(t, upstream.URL), "sk-real-key", "model", "openai")

	request := func(body io.ReadCloser, contentLength int64) int {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/chat/completions", body)
		req.Header.Set("Authorization", "Bearer virtual-tok")
		req.ContentLength = contentLength
		recorder := httptest.NewRecorder()
		p.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if code := request(failingRequestBody{}, -1); code != http.StatusBadRequest {
		t.Fatalf("unreadable body status = %d, want 400", code)
	}
	if code := request(io.NopCloser(strings.NewReader("{}")), (64<<20)+1); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body status = %d, want 413", code)
	}
	if upstreamCalls != 0 {
		t.Fatalf("invalid bodies reached upstream %d times", upstreamCalls)
	}
}

// TestCredentialProxyTokenStableAcrossRestarts: the virtual token derives
// from a persisted secret plus host/workspace/model identity, so a restarted
// desktop keeps the same route while distinct workspaces stay isolated.
func TestCredentialProxyTokenStableAcrossRestarts(t *testing.T) {
	seedBridgeTestHost(t, "box")
	a1 := &App{}
	t.Cleanup(a1.closeCredentialProxy)
	i1, err := a1.registerCredentialProxyRoute("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	a2 := &App{}
	t.Cleanup(a2.closeCredentialProxy)
	i2, err := a2.registerCredentialProxyRoute("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if i1.token == "" || i1.token != i2.token {
		t.Fatalf("token drifted across App instances: %q vs %q", i1.token, i2.token)
	}
	i3, err := a2.registerCredentialProxyRoute("other", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if i3.token == i1.token {
		t.Fatalf("different hosts share a token: %q", i1.token)
	}
	i4, err := a2.registerCredentialProxyRoute("box", "~/other")
	if err != nil {
		t.Fatal(err)
	}
	if i4.token == i1.token {
		t.Fatalf("different workspaces share a token: %q", i1.token)
	}
}

func TestCredentialProxyModelTokensKeepRoutesImmutable(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	one := credentialProxyModelTokenFor(secret, "box", "~/app", "provider/model-a")
	two := credentialProxyModelTokenFor(secret, "box", "~/app", "provider/model-b")
	again := credentialProxyModelTokenFor(secret, "box", "~/app", "provider/model-a")
	if one == two {
		t.Fatal("different models shared one mutable credential proxy route token")
	}
	if one != again {
		t.Fatal("the same model route token was not stable across registration")
	}
	if collision := credentialProxyModelTokenFor(secret, "box", "~/app", "provider:model-a"); collision == credentialProxyModelTokenFor(secret, "box", "~/app:provider", "model-a") {
		t.Fatal("length-framed route identities shared a token")
	}
}

func TestCredentialProxyReconnectRegistersTrackedWorkspaces(t *testing.T) {
	seedBridgeTestHost(t, "box")
	app := &App{}
	t.Cleanup(app.closeCredentialProxy)
	mgr := newDesktopRemoteManager(app)
	mgr.hosts["box"] = &managedHost{serves: map[string]*serveEntry{
		"~/app":   {},
		"~/other": {},
	}}
	info, err := mgr.registerTrackedCredentialRoutes(app, "box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := app.credentialProxyModelToken("box", "~/other", cfg.DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	app.credProxy.mu.Lock()
	defer app.credProxy.mu.Unlock()
	if info.token == "" || app.credProxy.routes[info.token] == nil || app.credProxy.routes[otherToken] == nil {
		t.Fatalf("tracked routes were not registered together: current=%q count=%d", info.token, len(app.credProxy.routes))
	}
}

func TestEnsureServerRejectsRemovedHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	client := newLifecycleSSHClient(nil)
	mgr := newDesktopRemoteManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr.hosts["removed"] = &managedHost{ctx: ctx, cancel: cancel, client: client, serves: map[string]*serveEntry{}}
	called := false
	mgr.ensureServe = func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error) {
		called = true
		return bootstrap.Result{}, nil
	}
	if _, _, err := mgr.EnsureServer(context.Background(), "removed", "~/app"); err == nil || !strings.Contains(err.Error(), "no longer configured") {
		t.Fatalf("EnsureServer removed host error = %v", err)
	}
	if called {
		t.Fatal("removed host reached remote bootstrap")
	}
}

// TestCredentialProxyAnthropicAuthShape: an anthropic-kind route swaps the
// virtual token for x-api-key (+ anthropic-version) instead of a bearer
// header.
func TestCredentialProxyAnthropicAuthShape(t *testing.T) {
	var gotKey, gotVersion, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	seedBridgeTestHost(t, "box")
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	port, err := a.credentialProxyPort()
	if err != nil {
		t.Fatal(err)
	}
	a.credProxy.setRoute("virtual-tok", mustParseURL(t, upstream.URL), "sk-real-key", "", "anthropic")
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port), strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer virtual-tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if gotKey != "sk-real-key" {
		t.Fatalf("x-api-key = %q, want the real key", gotKey)
	}
	if gotVersion == "" {
		t.Fatal("anthropic-version header missing")
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header leaked to the anthropic upstream: %q", gotAuth)
	}
}

// TestRewriteJSONModelGuards: a literal null body must pass through without
// panicking (assigning into a nil map would), and a non-JSON body stays
// untouched.
func TestRewriteJSONModelGuards(t *testing.T) {
	if got := rewriteJSONModel([]byte("null"), "m"); string(got) != "null" {
		t.Fatalf("null body rewritten: %q", got)
	}
	if got := rewriteJSONModel([]byte("not json"), "m"); string(got) != "not json" {
		t.Fatalf("non-JSON body rewritten: %q", got)
	}
	if got := rewriteJSONModel([]byte(`{"model":"a"}`), ""); string(got) != `{"model":"a"}` {
		t.Fatalf("empty model rewrote the body: %q", got)
	}
	if got := rewriteJSONModel([]byte(`{"model":"a"}`), "b"); !strings.Contains(string(got), `"model":"b"`) {
		t.Fatalf("model not rewritten: %q", got)
	}
}

// TestCredentialModeConfigRoundTrip pins the host entry field end to end.
func TestCredentialModeConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "p", Host: "127.0.0.1", CredentialMode: "local-proxy",
		})
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.RemoteHost("p")
	if !ok || !entry.CredentialProxyEnabled() {
		t.Fatalf("credential mode did not round-trip: %+v", entry)
	}
	if v := credentialModeView(entry); v != "local-proxy" {
		t.Fatalf("view mode = %q", v)
	}
	if n := normalizeCredentialMode("bogus"); n != "" {
		t.Fatalf("bogus mode normalized to %q", n)
	}
}
