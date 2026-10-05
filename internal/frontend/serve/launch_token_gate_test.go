package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestHubAuthDisabledGateHoldsEveryMutationSpelling(t *testing.T) {
	hub := NewHub(HubOptions{Serve: config.ServeConfig{AuthMode: "none"}})
	defer hub.Shutdown()
	bc := NewBroadcaster()
	rt, err := hub.Adopt(New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{}), bc)
	if err != nil {
		t.Fatal(err)
	}
	h := hub.Handler()
	tok := hub.auth.Token()
	cases := []struct {
		name, method, target string
		hdr                  map[string]string
		wantCode             string
	}{
		{"runtime-prefixed approve", http.MethodPost, runtimePrefix + rt.ID + "/approve", nil, codeLaunchTokenRequired},
		{"PUT", http.MethodPut, "/approve", nil, codeLaunchTokenRequired},
		{"PATCH", http.MethodPatch, "/inbox/items/x", nil, codeLaunchTokenRequired},
		{"DELETE", http.MethodDelete, "/sessions/x", nil, codeLaunchTokenRequired},
		{"PROPFIND", "PROPFIND", "/approve", nil, codeLaunchTokenRequired},
		{"override header", http.MethodPost, "/approve", map[string]string{"X-HTTP-Method-Override": "GET"}, codeLaunchTokenRequired},
		{"query token ignored", http.MethodPost, "/approve?token=" + tok, nil, codeLaunchTokenRequired},
		{"host route runtimes", http.MethodPost, "/runtimes", nil, codeLaunchTokenRequired},
		{"hub bearer", http.MethodPost, "/approve", map[string]string{"Authorization": "Bearer " + tok}, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(`{"allow":true}`))
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Content-Type", "application/json")
		for k, v := range c.hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var reason Reason
		_ = json.NewDecoder(rec.Body).Decode(&reason)
		if c.wantCode != "" && (rec.Code != http.StatusForbidden || reason.Code != c.wantCode) {
			t.Errorf("%s: %s %s = %d %q, want 403 %q", c.name, c.method, c.target, rec.Code, reason.Code, c.wantCode)
		}
		if c.wantCode == "" && reason.Code == codeLaunchTokenRequired {
			t.Errorf("%s: refused with launch token", c.name)
		}
	}
}

// A request reaching the hub through NewDeviceGate's public-path branch has
// device reach with an empty id; it must not count as a proven operator.
func TestUnpairedDeviceReachIsNotOperator(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/approve", nil)
	req = req.WithContext(withDeviceReach(req.Context(), "", 0))
	if provenOperator(req) {
		t.Fatal("unpaired device reach counted as operator")
	}
}

// With auth none a read addressed to a foreign name is refused, so a
// DNS-rebound page cannot read through a loopback listener.
func TestHubAuthDisabledRefusesForeignHost(t *testing.T) {
	hub := NewHub(HubOptions{Serve: config.ServeConfig{AuthMode: "none"}})
	defer hub.Shutdown()
	bc := NewBroadcaster()
	if _, err := hub.Adopt(New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{}), bc); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "rebind.attacker.example:8787"
	rec := httptest.NewRecorder()
	hub.Handler().ServeHTTP(rec, req)
	var reason Reason
	_ = json.NewDecoder(rec.Body).Decode(&reason)
	if rec.Code != http.StatusMisdirectedRequest || reason.Code != codeHostRejected {
		t.Fatalf("GET /status with Host=%s = %d %q, want 421 %q", req.Host, rec.Code, reason.Code, codeHostRejected)
	}
	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "127.0.0.1:8787"
	rec = httptest.NewRecorder()
	hub.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status on loopback = %d, want 200", rec.Code)
	}
	hub.AllowHost("agent.example.com")
	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Host = "agent.example.com"
	rec = httptest.NewRecorder()
	hub.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status on an allowed public host = %d, want 200", rec.Code)
	}
}

// The device gate pins Host to its own origin, so a stranger loading the page
// over the LAN is not a rebound request even though no pairing exists yet.
func TestHubAuthDisabledServesThePageThroughTheDeviceGate(t *testing.T) {
	hub := NewHub(HubOptions{Page: fstest.MapFS{"index.html": {Data: []byte("<title>studio</title>")}}})
	defer hub.Shutdown()
	gate := NewDeviceGate(hub.Handler(), DeviceGateOptions{Registry: NewDeviceRegistry(), Origin: "http://192.168.1.5:9000", Page: hub.opts.Page})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "192.168.1.5:9000"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpaired device page load = %d %s, want 200", rec.Code, rec.Body)
	}
}

// A wildcard bind answers for this machine's interface addresses, not for any
// name: a rebound page on 0.0.0.0 is still a foreign Host.
func TestAuthDisabledWildcardBindAdmitsOnlyInterfaceAddresses(t *testing.T) {
	previous := interfaceAddrs
	interfaceAddrs = func() []string { return []string{"192.168.1.5", "fe80::1"} }
	t.Cleanup(func() { interfaceAddrs = previous })
	var hosts hostAllowlist
	hosts.ListenOn("0.0.0.0:8787")
	for host, want := range map[string]bool{
		"192.168.1.5:8787":        true,
		"[fe80::1]:8787":          true,
		"localhost:8787":          true,
		"rebind.attacker.example": false,
		"10.0.0.9:8787":           false,
	} {
		if got := hosts.admits(host); got != want {
			t.Errorf("wildcard bind admits %q = %v, want %v", host, got, want)
		}
	}
}
