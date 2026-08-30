package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

// mcpAppsSandbox serves MCP Apps resources from per-server loopback origins:
// each Apps server gets its own 127.0.0.1 listener, so two servers' apps can
// never share cookies, storage, or an origin. The origin serves the outer
// sandbox relay page and proxies validated resources to the inner sandboxed
// iframe; the desktop webview never loads app HTML directly. A bind failure
// permanently degrades this desktop to the interactive MCP profile.
type mcpAppsSandbox struct {
	down atomic.Bool
	mu   sync.Mutex

	origins  map[string]*mcpAppOrigin
	bindings map[string]mcpAppBinding
}

type mcpAppBinding struct {
	tabID  string
	server string
	host   *plugin.Host
	ctrl   control.SessionAPI
}

type mcpAppOrigin struct {
	server   string
	listener net.Listener
	http     *http.Server
	nonce    string
}

// maxAppResourceBytes caps one decoded ui resource; maxAppPostMessageBytes
// caps one relayed frame.
const (
	maxAppResourceBytes    = 4 << 20
	maxAppPostMessageBytes = 8 << 20
	appResourceReadTimeout = 30 * time.Second
)

func (s *mcpAppsSandbox) available() bool { return !s.down.Load() }

func (s *mcpAppsSandbox) bind(token string, binding mcpAppBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindings == nil {
		s.bindings = map[string]mcpAppBinding{}
	}
	s.bindings[token] = binding
}

func (s *mcpAppsSandbox) binding(token string) (mcpAppBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[token]
	return binding, ok
}

func (s *mcpAppsSandbox) release(token string) (mcpAppBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[token]
	delete(s.bindings, token)
	return binding, ok
}

// appOriginURL returns the outer sandbox page URL for a server, binding the
// per-server listener on first use.
func (a *App) appOriginURL(server string) (string, error) {
	s := &a.mcpAppsSandbox
	if !s.available() {
		return "", fmt.Errorf("MCP Apps sandbox unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origins == nil {
		s.origins = map[string]*mcpAppOrigin{}
	}
	if o, ok := s.origins[server]; ok {
		return o.relayURL(), nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.down.Store(true)
		return "", fmt.Errorf("bind MCP Apps origin: %w", err)
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		_ = ln.Close()
		return "", fmt.Errorf("app origin nonce: %w", err)
	}
	o := &mcpAppOrigin{server: server, listener: ln, nonce: hex.EncodeToString(nonceBytes)}
	o.http = &http.Server{
		Handler:           o.mux(a),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	s.origins[server] = o
	go func() { _ = o.http.Serve(ln) }()
	if a.ctx != nil {
		a.goSafe("mcpAppOrigin:"+server, func() {
			<-a.ctx.Done()
			_ = o.http.Close()
		})
	}
	return o.relayURL(), nil
}

func (o *mcpAppOrigin) relayURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/sandbox?nonce=%s", o.listener.Addr().(*net.TCPAddr).Port, o.nonce)
}

func (o *mcpAppOrigin) mux(a *App) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/sandbox", o.serveRelayPage)
	mux.HandleFunc("/resource", o.serveResource(a))
	return mux
}

// outerSandboxRelay is the only page served at the loopback origin: a relay
// between the desktop webview (parent) and the inner sandboxed iframe. It
// hardens the channel: no top navigation, popup, object, or download; the
// instance nonce binds the first parent message; every relayed frame checks
// event.source; frames above the cap are refused; RPC before the inner frame
// loads is dropped.
const outerSandboxRelay = `<!doctype html>
<html><head><meta charset="utf-8"><title>MCP App</title>
<script>
(function () {
  var params = new URLSearchParams(location.search);
  var nonce = params.get("nonce");
  var src = params.get("src");
  var inner = null;
  try {
    var resource = new URL(src, location.href);
    if (resource.origin !== location.origin || resource.pathname !== "/resource") return;
    src = resource.pathname + resource.search;
  } catch (e) { return; }
  function frameSize(data) {
    try {
      var text = typeof data === "string" ? data : JSON.stringify(data);
      return new TextEncoder().encode(text).byteLength;
    } catch (e) { return %d + 1; }
  }
  window.addEventListener("message", function (event) {
    if (event.source === window.parent) {
      if (event.data && event.data.__mcpInit === nonce) {
        if (inner) return;
        inner = document.createElement("iframe");
        inner.setAttribute("sandbox", "allow-scripts");
        inner.setAttribute("src", src);
        document.body.appendChild(inner);
        return;
      }
      if (!inner || !inner.contentWindow || frameSize(event.data) > %d) return;
      try { inner.contentWindow.postMessage(event.data, "*"); } catch (e) {}
      return;
    }
    if (!inner || event.source !== inner.contentWindow) return;
    if (frameSize(event.data) > %d) return;
    try { window.parent.postMessage(event.data, "*"); } catch (e) {}
  });
}());
</script></head><body></body></html>`

func (o *mcpAppOrigin) serveRelayPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Query().Get("nonce") != o.nonce {
		http.Error(w, "unknown instance", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-src 'self'; script-src 'unsafe-inline'")
	fmt.Fprintf(w, outerSandboxRelay, maxAppPostMessageBytes, maxAppPostMessageBytes, maxAppPostMessageBytes)
}

// serveResource validates the instance token and digest, then serves the
// immutable resource snapshot captured when the App was opened. Only the
// inner sandboxed iframe loads this copy.
func (o *mcpAppOrigin) serveResource(a *App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := r.URL.Query().Get("token")
		binding, ok := a.mcpAppsSandbox.binding(token)
		if !ok || binding.host == nil || binding.server != o.server {
			http.Error(w, "unknown instance", http.StatusForbidden)
			return
		}
		inst, ok := binding.host.LookupAppInstance(token)
		if !ok || inst.Server != o.server || !strings.HasPrefix(inst.ResourceURI, "ui://") {
			a.mcpAppsSandbox.release(token)
			http.Error(w, "unknown instance", http.StatusForbidden)
			return
		}
		snapshot, ok := binding.host.AppResource(token)
		if !ok || r.URL.Query().Get("digest") != snapshot.Digest || len(snapshot.Content) > maxAppResourceBytes || !isAppHTMLMimeType(snapshot.MIME) {
			http.Error(w, "resource unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", appResourceCSP(snapshot.CSP))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-App-Sha256", snapshot.Digest)
		_, _ = io.WriteString(w, snapshot.Content)
	}
}

func isAppHTMLMimeType(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	return mime == "" || mime == "text/html" || strings.HasPrefix(mime, "text/html;") ||
		strings.Contains(mime, "profile=mcp-app")
}

// appResourceCSP defaults every channel to deny and extends it only with exact
// declared origins. Wildcards, credentials, paths, and undeclared hosts are
// refused; connectDomains additionally accepts exact ws/wss origins.
func appResourceCSP(csp map[string][]string) string {
	connect := allowedCSPSources(csp, []string{"connectDomains", "connect-src"}, []string{"http", "https", "ws", "wss"})
	resources := allowedCSPSources(csp, []string{"resourceDomains", "resource-src"}, []string{"http", "https"})
	frames := allowedCSPSources(csp, []string{"frameDomains", "frame-src"}, []string{"http", "https"})
	bases := allowedCSPSources(csp, []string{"baseUriDomains", "base-uri"}, []string{"http", "https"})
	directives := []string{
		"default-src 'none'",
		"object-src 'none'",
		"script-src " + cspWithBase("'unsafe-inline'", resources),
		"style-src " + cspWithBase("'unsafe-inline'", resources),
		"img-src " + cspWithBase("data:", resources),
		"font-src " + cspOr("'none'", resources),
		"media-src " + cspOr("'none'", resources),
		"connect-src " + cspOr("'none'", connect),
		"frame-src " + cspOr("'none'", frames),
		"base-uri " + cspOr("'self'", bases),
		"frame-ancestors 'self'",
	}
	return strings.Join(directives, "; ")
}

func cspOr(fallback string, sources []string) string {
	if len(sources) == 0 {
		return fallback
	}
	return strings.Join(sources, " ")
}

func cspWithBase(base string, sources []string) string {
	if len(sources) == 0 {
		return base
	}
	return base + " " + strings.Join(sources, " ")
}

func allowedCSPSources(csp map[string][]string, keys, schemes []string) []string {
	var allowed []string
	for _, key := range keys {
		for _, source := range csp[key] {
			if cspOriginAllowed(source, schemes) {
				allowed = append(allowed, strings.TrimSpace(source))
			}
		}
	}
	slices.Sort(allowed)
	return slices.Compact(allowed)
}

func cspOriginAllowed(origin string, schemes []string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" || strings.ContainsAny(origin, "*'; ") {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && slices.Contains(schemes, u.Scheme) && u.Host != "" && u.User == nil &&
		(u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}

func resourceDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
