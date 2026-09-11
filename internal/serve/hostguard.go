package serve

import (
	"net"
	"net/http"
	"strings"
)

// hostGateState captures which Host headers hostGuard accepts, derived from
// the listen address. Wildcard and non-loopback binds deliberately expose the
// server beyond this machine, so Host policing there adds nothing and is off.
type hostGateState struct {
	behindProxy bool
	allowAny    bool
	listenHost  string // specific, non-wildcard host the server is bound to
}

// setListenAddr derives hostGate from the address Run-style entry points will
// listen on. It must be called before Handler(); a later call only affects
// servers whose Handler is rebuilt.
func (s *Server) setListenAddr(addr string) {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	s.hostGate = hostGateState{
		behindProxy: s.auth != nil && s.auth.behindProxy &&
			(s.auth.mode == authToken || s.auth.mode == authPassword),
		listenHost: strings.ToLower(host),
		allowAny:   isUnspecifiedHost(host),
	}
}

func isUnspecifiedHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// hostGuard rejects requests whose Host header names neither a loopback
// interface nor the address serve actually listens on.
//
// csrfGuard's application/json requirement only holds while an attacker's page
// stays cross-origin: a DNS-rebinding page is served from evil.example, which
// is then re-pointed at 127.0.0.1, making every subsequent fetch same-origin —
// no preflight, any Content-Type, and full read access to responses. Such a
// page can drive the unauthenticated agent endpoints (POST /bypass, /submit)
// and read /history verbatim. Pinning Host to the interfaces we serve breaks
// that: the rebound name never matches the allowlist.
//
// Exemptions: behind_proxy deployments send the reverse proxy's public
// hostname and must instead run an authenticated mode; wildcard / non-loopback
// binds (allowAny) intentionally expose the server. Requests with no Host at
// all (raw HTTP/1.0 clients) pass — nothing to validate.
func (s *Server) hostGuard(next http.Handler) http.Handler {
	gate := s.hostGate
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		if host == "" || gate.behindProxy || gate.allowAny ||
			isLoopbackHost(host) || host == gate.listenHost {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "misdirected request: Host is not a serve listen address",
			http.StatusMisdirectedRequest)
	})
}

// csrfGuard rejects state-changing requests that don't carry a JSON content type.
// The command endpoints have no auth and bind to localhost, so a page the user
// visits could otherwise drive them with a simple cross-origin POST (text/plain,
// no preflight) — submitting prompts or auto-approving tool calls. Requiring
// application/json forces a CORS preflight the unauthenticated server never
// answers, blocking cross-site requests; the same-origin frontend (which always
// sends JSON) is unaffected.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			if i := strings.IndexByte(ct, ';'); i >= 0 {
				ct = ct[:i]
			}
			if !strings.EqualFold(strings.TrimSpace(ct), "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
