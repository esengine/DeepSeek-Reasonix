package servepool

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// HostHook lets the embedding host (the desktop) participate in session
// ownership handoff (#8987): when a proxied takeover-session request hits a
// 409 "held by another runtime" and the holder is the gateway's own host,
// the hook releases the session (with a handoff reservation) so the gateway
// can retry. A nil hook disables the retry (standalone serve --pool host).
type HostHook interface {
	// HandoffSession releases the session (identified by its project id and
	// session name) to targetWriterID. The host resolves the absolute
	// session path itself. Return nil when the handoff was granted.
	HandoffSession(projectID, sessionName, targetWriterID string) error
}

// Gateway is the single-entry HTTP surface in front of the serve pool.
// Remote clients (GrandCouncil, Reasonix-web) talk to exactly one endpoint;
// /p/<project-id>/* requests are lazily spawned and reverse-proxied to the
// project's 127.0.0.1 serve. All endpoints require the gateway bearer token.
type Gateway struct {
	mgr   *Manager
	token string
	hook  HostHook
	proxy map[string]*httputil.ReverseProxy
}

// NewGateway builds the gateway handler. token must be a non-empty secret
// (desktop-generated and shown in settings; clients configure it once).
// hook enables takeover-session retry for host-held sessions (nil = off).
func NewGateway(mgr *Manager, token string, hook HostHook) *Gateway {
	if strings.TrimSpace(token) == "" {
		token = newToken()
	}
	return &Gateway{mgr: mgr, token: token, hook: hook, proxy: map[string]*httputil.ReverseProxy{}}
}

// Token returns the gateway bearer token (for UI display / first setup).
func (g *Gateway) Token() string { return g.token }

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/manifest":
		g.handleManifest(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/projects/open":
		g.handleOpen(w, r)
	case strings.HasPrefix(r.URL.Path, "/p/"):
		g.handleProxy(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (g *Gateway) authorized(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	given := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if given == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(given), []byte(g.token)) == 1
}

func (g *Gateway) handleManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, g.mgr.Projects())
}

func (g *Gateway) handleOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		http.Error(w, `missing "id"`, http.StatusBadRequest)
		return
	}
	if err := g.mgr.Open(strings.TrimSpace(body.ID)); err != nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "running"})
}

// handleProxy routes /p/<id>/<rest> to the project's serve, lazily spawning
// it on first use, stripping the /p/<id> prefix before forwarding.
func (g *Gateway) handleProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/p/")
	id, tail, ok := strings.Cut(rest, "/")
	if !ok {
		http.Error(w, "project route must be /p/<id>/...", http.StatusBadRequest)
		return
	}
	id = strings.TrimSpace(id)
	if err := g.mgr.Open(id); err != nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	g.mgr.Touch(id)
	port := g.mgr.Port(id)
	if port == 0 {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "project serve not ready"})
		return
	}
	proxy := g.proxyFor(id, port)
	// Rewrite the incoming request onto the target path (prefix stripped).
	out := r.Clone(r.Context())
	out.URL.Path = "/" + tail
	if out.URL.RawPath != "" {
		out.URL.RawPath = "/" + strings.TrimPrefix(out.URL.RawPath, "/p/"+id+"/")
	}
	// Forward the per-project token so the serve's auth=token accepts us.
	if tok := g.mgr.Token(id); tok != "" {
		out.Header.Set("Authorization", "Bearer "+tok)
	}
	proxy.ServeHTTP(w, out)
}

func (g *Gateway) proxyFor(id string, port int) *httputil.ReverseProxy {
	if p, ok := g.proxy[id]; ok {
		return p
	}
	target, _ := url.Parse("http://127.0.0.1:" + itoa(port))
	p := httputil.NewSingleHostReverseProxy(target)
	p.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}
	if g.hook != nil {
		p.Transport = &takeoverRetryTransport{
			base:      http.DefaultTransport,
			hook:      g.hook,
			projectID: id,
		}
	}
	g.proxy[id] = p
	return p
}

// takeoverRetryTransport wraps the project-serve transport so a
// takeover-session 409 ("held by another runtime") can be resolved by the
// host's HandoffSession hook and retried once (#8987).
type takeoverRetryTransport struct {
	base      http.RoundTripper
	hook      HostHook
	projectID string
}

func (t *takeoverRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	isTakeover := strings.HasSuffix(req.URL.Path, "/takeover-session")
	var body []byte
	var bodyErr error
	if isTakeover && req.Body != nil {
		body, bodyErr = io.ReadAll(req.Body)
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || !isTakeover || resp.StatusCode != http.StatusConflict {
		return resp, err
	}
	if bodyErr != nil || len(body) == 0 {
		return resp, err
	}
	// The project serve cannot hand us the session because its holder is the
	// gateway's own host (desktop). Ask the host to release it, then retry.
	var target struct {
		Name string `json:"name"`
		From string `json:"from,omitempty"`
	}
	_ = json.Unmarshal(body, &target)
	if strings.TrimSpace(target.Name) == "" {
		return resp, err
	}
	if err := t.hook.HandoffSession(t.projectID, target.Name, target.From); err != nil {
		// Keep the 409 response (with its body) intact for the client; the
		// hook error is diagnostic only.
		return resp, nil
	}
	req2 := req.Clone(req.Context())
	req2.Body = io.NopCloser(bytes.NewReader(body))
	req2.ContentLength = int64(len(body))
	return t.base.RoundTrip(req2)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
