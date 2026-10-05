package serve

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// Each refusal names the invariant that fired. One shared code would leave a
// test unable to tell a rebinding defense that held from a credential check
// that happened to answer first.
const (
	codeLoopbackHost          = "loopback.host_rejected"
	codeLoopbackOrigin        = "loopback.origin_rejected"
	codeLoopbackUnauthorized  = "loopback.unauthorized"
	codeLoopbackMisconfigured = "loopback.misconfigured"
)

// LoopbackGateOptions is the whole policy. Both fields belong to the host that
// opened the listener: no configuration file contributes to either, so nothing
// a user can edit turns this boundary off.
type LoopbackGateOptions struct {
	// Token is the ephemeral credential, compared against the TokenCookie the
	// host set. Minted per launch and never persisted.
	Token string
	// Origin is what this listener answers for, as an IP literal:
	// http://127.0.0.1:<port>. LoopbackOrigin builds it from the listener.
	Origin string
}

// ListenLoopback opens the only listener this gate is meant to guard. Spelled
// out rather than ":0" or "localhost:0" so no caller can widen the socket to a
// wildcard address or to a name a resolver gets to answer for.
func ListenLoopback() (net.Listener, error) {
	return net.Listen("tcp4", "127.0.0.1:0")
}

// LoopbackOrigin is the origin a ListenLoopback listener answers for.
func LoopbackOrigin(ln net.Listener) string {
	if ln == nil {
		return ""
	}
	return "http://" + ln.Addr().String()
}

// NewLoopbackGate guards a control plane that has become reachable through a
// socket: a request must be addressed to this listener, must name this listener
// as its origin when it changes anything, and must carry the host's credential.
// A gate built without both halves of its policy refuses everything.
func NewLoopbackGate(next http.Handler, opts LoopbackGateOptions) http.Handler {
	authority, ok := loopbackAuthority(opts.Origin)
	if !ok || opts.Token == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			refuse(w, http.StatusInternalServerError, codeLoopbackMisconfigured,
				"this gate was built without a loopback origin and a token", nil)
		})
	}
	origin, token := "http://"+authority, opts.Token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Outermost first: a caller that reached the socket under another name
		// learns nothing about the credential, because the address is settled
		// before the token is ever compared.
		if !strings.EqualFold(r.Host, authority) {
			refuse(w, http.StatusForbidden, codeLoopbackHost, "this listener does not answer for that host", nil)
			return
		}
		if !loopbackOriginAllowed(r, origin) {
			refuse(w, http.StatusForbidden, codeLoopbackOrigin, "that origin may not reach this listener", nil)
			return
		}
		if !loopbackTokenPresented(r, token) {
			refuse(w, http.StatusForbidden, codeLoopbackUnauthorized, "this listener needs the host's credential", nil)
			return
		}
		// w passes through undecorated: /events asserts http.Flusher on it, and
		// a wrapper that did not forward Flush would turn every stream into a
		// response the page only sees once the turn is over.
		next.ServeHTTP(w, withHostGated(r))
	})
}

// loopbackOriginAllowed splits the rule by what the request can do. A read may
// arrive without an origin — a top-level navigation sends none — but anything
// that changes state must name this listener rather than lean on the cookie.
func loopbackOriginAllowed(r *http.Request, want string) bool {
	sent := r.Header.Get("Origin")
	if sent == "" {
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return sent == want
}

// loopbackTokenPresented reads the cookie the host set. Cookie only: the query
// token the web gate also accepts would put the credential into request lines,
// history and referrers, which is what setting it host-side avoided.
func loopbackTokenPresented(r *http.Request, token string) bool {
	c, err := r.Cookie(TokenCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}

// loopbackAuthority is the host:port an origin answers for, and reports whether
// it is one this gate may guard. An IP literal on the loopback interface only —
// a name would leave a resolver deciding what the gate protects.
func loopbackAuthority(origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "" || u.RawQuery != "" {
		return "", false
	}
	ap, err := netip.ParseAddrPort(u.Host)
	if err != nil || !ap.Addr().IsLoopback() || ap.Port() == 0 {
		return "", false
	}
	return u.Host, true
}
