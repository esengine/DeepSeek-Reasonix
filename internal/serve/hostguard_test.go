package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

// The DNS-rebinding scenario from the advisory: a page served from
// evil.example is re-pointed at 127.0.0.1, so its fetches arrive same-origin
// with Host: evil.example:8787. hostGuard must reject them before csrfGuard
// or any route sees the request — the content-type check is worthless once
// the attacker is same-origin.
func TestHostGuardBlocksReboundHost(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	h := s.Handler()

	for _, host := range []string{"evil.example", "evil.example:8787", "10.0.0.99", "[::ffff:10.0.0.99]:8787"} {
		req := httptest.NewRequest(http.MethodPost, "/bypass", strings.NewReader(`{"on":true}`))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Fatalf("POST /bypass with Host %q = %d, want %d", host, rec.Code, http.StatusMisdirectedRequest)
		}

		req = httptest.NewRequest(http.MethodGet, "/history", nil)
		req.Host = host
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Fatalf("GET /history with Host %q = %d, want %d", host, rec.Code, http.StatusMisdirectedRequest)
		}
	}
}

func TestHostGuardAllowsLoopbackHosts(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	h := s.Handler()

	for _, host := range []string{"127.0.0.1", "127.0.0.1:8787", "localhost", "localhost:8787", "[::1]:8787", ""} {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusMisdirectedRequest {
			t.Fatalf("GET /status with Host %q rejected as misdirected", host)
		}
	}
}

func TestHostGuardAllowsListenHostAndWildcardBind(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	s.setListenAddr("10.1.2.3:8787")
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "10.1.2.3:8787"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusMisdirectedRequest {
		t.Fatal("request via the specific listen host was rejected")
	}

	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "192.168.0.20:8787"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatal("request via an unrelated LAN host was accepted")
	}

	// A wildcard bind deliberately exposes the server; Host policing is off.
	wild := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	wild.setListenAddr("0.0.0.0:8787")
	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "anything.example:8787"
	rec = httptest.NewRecorder()
	wild.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusMisdirectedRequest {
		t.Fatal("wildcard bind rejected a foreign Host")
	}
}

func TestHostGuardExemptsBehindProxy(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{
		AuthMode:    "token",
		Token:       "serve-token",
		BehindProxy: true,
	})
	s.setListenAddr("127.0.0.1:8787")
	h := s.Handler()

	// The public hostname passes the host guard; auth still applies.
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "agent.internal.example:8787"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusMisdirectedRequest {
		t.Fatal("behind_proxy deployment rejected on public hostname")
	}
}

func TestHostGuardDoesNotExemptUnauthenticatedProxyMode(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{
		AuthMode:    "none",
		BehindProxy: true,
	})
	s.setListenAddr("127.0.0.1:8787")

	req := httptest.NewRequest(http.MethodGet, "/history", nil)
	req.Host = "evil.example:8787"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("GET /history = %d, want %d", rec.Code, http.StatusMisdirectedRequest)
	}
}

func TestSetListenAddrBracketedIPv6(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	s.setListenAddr("[::1]:8787")
	if s.hostGate.listenHost != "::1" {
		t.Fatalf("listenHost = %q, want ::1", s.hostGate.listenHost)
	}
	if s.hostGate.allowAny {
		t.Fatal("::1 bind must not be treated as wildcard")
	}
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "[::1]:8787"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusMisdirectedRequest {
		t.Fatal("loopback IPv6 listen host rejected")
	}
}
