package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
)

// Local-proxy mode tunnels model calls to this desktop, which swaps a scoped
// virtual token for the real provider key. The real key never leaves desktop.

// credentialProxyProviderName is the provider entry the bootstrap installs in
// the remote config; the serve launches with --model <name>.
const credentialProxyProviderName = "reasonix-desktop-proxy"

type credProxyRoute struct {
	proxy *httputil.ReverseProxy
	model string
}

// credentialProxy is the desktop-side key holder: a loopback HTTP endpoint
// that authenticates requests by virtual token and forwards them to the real
// provider with the real key. One instance serves the whole app.
type credentialProxy struct {
	mu     sync.Mutex
	ln     net.Listener
	server *http.Server
	port   int
	routes map[string]*credProxyRoute
}

func (p *credentialProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Unauthenticated liveness endpoint for the desktop's reverse-tunnel
	// probe: the listener only exists behind the SSH reverse forward, so a
	// 204 here proves serve → remote loopback → tunnel → desktop end to end.
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	p.mu.Lock()
	route := p.routes[token]
	routeCount := len(p.routes)
	p.mu.Unlock()
	if route == nil {
		log.Printf("[remote] credProxy: rejected %s %s routeCount=%d", r.Method, r.URL.Path, routeCount)
		http.Error(w, "invalid credential proxy token", http.StatusUnauthorized)
		return
	}
	if route.model != "" && r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		const rewriteLimit = 64 << 20
		if r.ContentLength > rewriteLimit {
			http.Error(w, "credential proxy request body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		buffered, err := io.ReadAll(io.LimitReader(r.Body, rewriteLimit+1))
		switch {
		case err != nil:
			_ = r.Body.Close()
			http.Error(w, "credential proxy could not read request body", http.StatusBadRequest)
			return
		case int64(len(buffered)) > rewriteLimit:
			_ = r.Body.Close()
			http.Error(w, "credential proxy request body is too large", http.StatusRequestEntityTooLarge)
			return
		default:
			_ = r.Body.Close()
			body := rewriteJSONModel(buffered, route.model)
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
	}
	route.proxy.ServeHTTP(w, r)
}

func rewriteJSONModel(body []byte, model string) []byte {
	if model == "" || len(body) == 0 {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		// Unparseable or a literal null body ("null" decodes into a nil
		// map): assigning into nil would panic, and there is nothing to
		// rewrite — pass the body through untouched.
		return body
	}
	payload["model"] = model
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func (p *credentialProxy) setRoute(token string, upstream *url.URL, apiKey, model, kind string) {
	if kind == "" {
		kind = "openai"
	}
	proxy := &httputil.ReverseProxy{FlushInterval: -1}
	proxy.Rewrite = func(req *httputil.ProxyRequest) {
		req.SetURL(upstream)
		for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Via"} {
			req.Out.Header.Del(header)
		}
		if kind == "anthropic" {
			req.Out.Header.Del("Authorization")
			req.Out.Header.Set("x-api-key", apiKey)
			req.Out.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Out.Header.Del("x-api-key")
			req.Out.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[token] = &credProxyRoute{proxy: proxy, model: model}
}

func (p *credentialProxy) close() {
	p.mu.Lock()
	server, listener := p.server, p.ln
	p.server, p.ln = nil, nil
	p.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
}

func bearerToken(header string) string {
	prefix, value, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

// credentialProxyPort returns the proxy's loopback port, starting the proxy
// on first use.
func (a *App) credentialProxyPort() (int, error) {
	a.credProxyMu.Lock()
	defer a.credProxyMu.Unlock()
	if a.credProxy != nil {
		return a.credProxy.port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("credential proxy: listen: %w", err)
	}
	p := &credentialProxy{ln: ln, port: ln.Addr().(*net.TCPAddr).Port, routes: map[string]*credProxyRoute{}}
	server := &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	p.server = server
	a.credProxy = p
	a.goSafe("credentialProxy", func() { _ = server.Serve(ln) })
	return p.port, nil
}

func (a *App) closeCredentialProxy() {
	a.credProxyMu.Lock()
	defer a.credProxyMu.Unlock()
	if a.credProxy != nil {
		a.credProxy.close()
		a.credProxy = nil
	}
}

// credentialProxySecret loads (creating on first use) the persisted random
// secret every virtual token derives from. Rotating it revokes all tokens.
func (a *App) credentialProxySecret() (string, error) {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	p, err := updateRemotePrefsLocked(func(p *remotePrefs) (bool, error) {
		if p.CredentialProxySecret != "" {
			return false, nil
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return false, fmt.Errorf("credential proxy: generate secret: %w", err)
		}
		p.CredentialProxySecret = hex.EncodeToString(buf)
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("credential proxy: persist secret: %w", err)
	}
	return p.CredentialProxySecret, nil
}

// credentialProxyModelTokenFor gives each staged model an immutable route.
// A controller already running with the previous virtual token therefore keeps
// its old upstream for the whole turn while Serve builds and publishes the new
// controller. This is the cross-process half of failure-atomic model switches.
func credentialProxyModelTokenFor(secret, hostID, workspace, modelRef string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("reasonix-credential-proxy-model:v2"))
	for _, field := range []string{hostID, workspace, modelRef} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = mac.Write(size[:])
		_, _ = mac.Write([]byte(field))
	}
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

func (a *App) credentialProxyModelToken(hostID, workspace, modelRef string) (string, error) {
	secret, err := a.credentialProxySecret()
	if err != nil {
		return "", err
	}
	return credentialProxyModelTokenFor(secret, hostID, workspace, modelRef), nil
}

// credentialProxyRouteInfo is everything a serve bootstrap needs to install
// the desktop hop on the remote: the virtual token, the model name and
// provider kind the remote provider entry should carry, and the proxy's
// loopback port.
type credentialProxyRouteInfo struct {
	token string
	model string
	kind  string
	port  int
}

// proxyUpstream is the resolved desktop-side provider a route forwards to.
type proxyUpstream struct {
	apiKey string
	url    *url.URL
	model  string
	kind   string
}

// resolveProxyProvider resolves a desktop model ref into the upstream the
// credential proxy should forward to, including the auth-header shape its
// provider kind expects.
func resolveProxyProvider(cfg *config.Config, ref string) (proxyUpstream, error) {
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return proxyUpstream{}, fmt.Errorf("credential proxy: model %q has no provider", ref)
	}
	apiKey := config.ResolveCredential(entry.APIKeyEnv).Value
	if apiKey == "" {
		return proxyUpstream{}, fmt.Errorf("credential proxy: the local provider credential is not configured")
	}
	base := strings.TrimSpace(entry.BaseURL)
	if base == "" {
		base = "https://api.openai.com"
	}
	upstream, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return proxyUpstream{}, fmt.Errorf("credential proxy: provider base_url: %w", err)
	}
	if (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Host == "" || upstream.User != nil || upstream.Fragment != "" {
		return proxyUpstream{}, fmt.Errorf("credential proxy: provider base_url must be an http(s) URL without credentials or a fragment")
	}
	kind := strings.TrimSpace(entry.Kind)
	if kind == "" {
		kind = "openai"
	}
	return proxyUpstream{apiKey: apiKey, url: upstream, model: entry.Model, kind: kind}, nil
}

// registerCredentialProxyRoute binds one workspace token to the current
// desktop default provider without exposing its real key to the remote.
func (a *App) registerCredentialProxyRoute(hostID, workspace string) (credentialProxyRouteInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return credentialProxyRouteInfo{}, err
	}
	ref := strings.TrimSpace(cfg.DefaultModel)
	if workspaceModel := a.desktopModelForWorkspace(hostID, workspace); workspaceModel != "" {
		ref = workspaceModel
	}
	return a.applyCredentialProxyModel(hostID, workspace, ref)
}

// desktopModelForWorkspace deterministically selects the newest tab-owned
// model for a workspace; map iteration order must never choose a route.
func (a *App) desktopModelForWorkspace(hostID, workspace string) string {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	var selected string
	var selectedSeq uint64
	for _, tab := range a.remoteTabs {
		if tab == nil || tab.ref.HostID != hostID || tab.ref.Workspace != workspace || strings.TrimSpace(tab.model) == "" {
			continue
		}
		if tab.modelSeq >= selectedSeq {
			selected, selectedSeq = tab.model, tab.modelSeq
		}
	}
	return selected
}

func (a *App) applyCredentialProxyModel(hostID, workspace, ref string) (credentialProxyRouteInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return credentialProxyRouteInfo{}, err
	}
	up, err := resolveProxyProvider(cfg, ref)
	if err != nil {
		return credentialProxyRouteInfo{}, err
	}
	port, err := a.credentialProxyPort()
	if err != nil {
		return credentialProxyRouteInfo{}, err
	}
	a.credProxyMu.Lock()
	proxy := a.credProxy
	a.credProxyMu.Unlock()
	if proxy == nil {
		return credentialProxyRouteInfo{}, fmt.Errorf("credential proxy: not running")
	}
	// Route tokens include the canonical desktop model ref. Never mutate the
	// route held by an in-flight controller during a model switch.
	token, err := a.credentialProxyModelToken(hostID, workspace, ref)
	if err != nil {
		return credentialProxyRouteInfo{}, err
	}
	proxy.setRoute(token, up.url, up.apiKey, up.model, up.kind)
	return credentialProxyRouteInfo{token: token, model: up.model, kind: up.kind, port: port}, nil
}

// credentialModeView returns the host entry's normalized credential mode for
// views ("" reads as "remote" — the default).
func credentialModeView(h config.RemoteHostEntry) string {
	if h.CredentialProxyEnabled() {
		return "local-proxy"
	}
	return "remote"
}

// normalizeCredentialMode validates an input credential mode.
func normalizeCredentialMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "local-proxy":
		return "local-proxy"
	default:
		return ""
	}
}
