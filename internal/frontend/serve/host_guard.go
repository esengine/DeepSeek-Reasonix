package serve

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// codeHostRejected refuses an auth-disabled request addressed to a name this
// listener was not bound to: a DNS-rebound page reaches loopback under its
// own name, and nothing else stops it reading what an open serve answers.
const codeHostRejected = "serve.host_rejected"

// hostAllowlist is which Host headers an auth-disabled serve answers. Shared by
// a hub and every server it adopts, like the auth gate that holds it.
type hostAllowlist struct {
	mu    sync.Mutex
	names map[string]bool
}

// ListenOn records the address serve is bound to. A wildcard bind answers for
// this machine's interface addresses as IP literals, never for a name a
// resolver could point here.
func (h *hostAllowlist) ListenOn(addr string) {
	host := hostOnlyName(addr)
	ip := net.ParseIP(host)
	h.mu.Lock()
	defer h.mu.Unlock()
	if ip == nil || !ip.IsUnspecified() {
		h.addLocked(host)
		return
	}
	for _, a := range interfaceAddrs() {
		h.addLocked(a)
	}
}

// interfaceAddrs lists this machine's interface IPs; tests replace it.
var interfaceAddrs = func() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP.String())
		}
	}
	return out
}

// Allow adds a name serve answers besides its bound address, such as the host
// of --public-url a proxy forwards.
func (h *hostAllowlist) Allow(host string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addLocked(hostOnlyName(host))
}

func (h *hostAllowlist) addLocked(host string) {
	if host == "" {
		return
	}
	if h.names == nil {
		h.names = map[string]bool{}
	}
	h.names[host] = true
}

func (h *hostAllowlist) admits(hostport string) bool {
	host := hostOnlyName(hostport)
	if host == "" || isLoopbackHost(host) {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[host]
}

func hostOnlyName(hostport string) string {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

// AllowHost lets an auth-disabled server answer requests addressed to host.
func (s *Server) AllowHost(host string) { s.auth.hosts.Allow(host) }

// hostGuard applies the allowlist while authentication is off. A request that
// carries the launch token, or passed a gate that pins Host itself (loopback,
// device, in-process), is not a rebound page and passes.
func (ag *authGate) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, viaDeviceGate := DeviceOf(r.Context())
		if ag.mode == authNone && !ag.hosts.admits(r.Host) && !viaDeviceGate && !provenOperator(r) && !ag.presentsLaunchToken(r) {
			refuse(w, http.StatusMisdirectedRequest, codeHostRejected, "this serve does not answer for that host", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
