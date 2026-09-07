package servepool

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Gateway is the single-entry HTTP surface in front of the serve pool.
// Remote clients (GrandCouncil, Reasonix-web) talk to exactly one endpoint;
// /p/<project-id>/* requests are lazily spawned and reverse-proxied to the
// project's 127.0.0.1 serve. All endpoints require the gateway bearer token.
type Gateway struct {
	mgr   *Manager
	token string
	proxy map[string]*httputil.ReverseProxy
}

// NewGateway builds the gateway handler. token must be a non-empty secret
// (desktop-generated and shown in settings; clients configure it once).
func NewGateway(mgr *Manager, token string) *Gateway {
	if strings.TrimSpace(token) == "" {
		token = newToken()
	}
	return &Gateway{mgr: mgr, token: token, proxy: map[string]*httputil.ReverseProxy{}}
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
	g.proxy[id] = p
	return p
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
