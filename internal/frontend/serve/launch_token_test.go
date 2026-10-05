package serve

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// operatorHandler stands in for the operator's frontend, which holds the
// launch token that mutations require.
func operatorHandler(s interface {
	Handler() http.Handler
	AuthToken() string
}) http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+s.AuthToken())
		h.ServeHTTP(w, r)
	})
}

func postLaunchJSON(t *testing.T, url, body string, header http.Header) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	maps.Copy(req.Header, header)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reason Reason
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	return resp.StatusCode, reason.Code
}

func TestAuthDisabledRefusesMutationsWithoutLaunchToken(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{AuthMode: "none"}).Handler())
	defer srv.Close()

	for _, route := range []string{"/approve", "/plan-decision", "/answer", "/bypass", "/tool-approval-mode", "/auto-approve-tools", "/workspace-trust", "/permissions", "/permissions/remembered/revoke", "/sandbox", "/hooks", "/submit"} {
		status, code := postLaunchJSON(t, srv.URL+route, `{"id":"1","allow":true,"session":true}`, nil)
		if status != http.StatusForbidden || code != codeLaunchTokenRequired {
			t.Errorf("POST %s without launch token = %d %q, want 403 %q", route, status, code, codeLaunchTokenRequired)
		}
	}
	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /status with auth disabled = %d, want 200: reads stay open", resp.StatusCode)
	}
}

func TestAuthDisabledAcceptsMutationWithLaunchToken(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	s := New(ctrl, bc, config.ServeConfig{AuthMode: "none"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if s.AuthToken() == "" {
		t.Fatal("auth-disabled serve has no launch token")
	}
	if status, _ := postLaunchJSON(t, srv.URL+"/approve", `{"allow":true}`, http.Header{"Authorization": {"Bearer " + s.AuthToken()}}); status != http.StatusBadRequest {
		t.Fatalf("POST /approve with bearer launch token = %d, want the handler's 400 for a missing id", status)
	}
	if status, code := postLaunchJSON(t, srv.URL+"/approve", `{"allow":true}`, http.Header{"Authorization": {"Bearer wrong"}}); status != http.StatusForbidden || code != codeLaunchTokenRequired {
		t.Fatalf("POST /approve with a wrong token = %d %q, want 403", status, code)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/token", strings.NewReader(`{"token":"`+s.AuthToken()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieToken {
			cookie = c
		}
	}
	if resp.StatusCode != http.StatusNoContent || cookie == nil {
		t.Fatalf("launch token bootstrap = %d cookie=%v, want 204 with %s", resp.StatusCode, cookie, cookieToken)
	}
	if status, _ := postLaunchJSON(t, srv.URL+"/approve", `{"allow":true}`, http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}); status != http.StatusBadRequest {
		t.Fatalf("POST /approve with launch token cookie = %d, want the handler's 400", status)
	}
}

func TestAuthDisabledHonoursConfiguredToken(t *testing.T) {
	s := New(control.New(control.Options{Sink: NewBroadcaster()}), nil, config.ServeConfig{AuthMode: "none", Token: "proxy-held"})
	if s.AuthToken() != "proxy-held" {
		t.Fatalf("launch token = %q, want the configured token a fronting proxy injects", s.AuthToken())
	}
}

func TestTokenModeAcceptsBearerLaunchToken(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	s := New(ctrl, bc, config.ServeConfig{AuthMode: "token"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if status, _ := postLaunchJSON(t, srv.URL+"/approve", `{"id":"1","allow":true}`, nil); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST /approve in token mode = %d, want 401", status)
	}
	if status, _ := postLaunchJSON(t, srv.URL+"/approve", `{"allow":true}`, http.Header{"Authorization": {"Bearer " + s.AuthToken()}}); status != http.StatusBadRequest {
		t.Fatalf("POST /approve with bearer token = %d, want the handler's 400", status)
	}
}

func TestHubWithAuthDisabledAdmitsOnlyGatedOrInProcessMutations(t *testing.T) {
	hub := NewHub(HubOptions{})
	defer hub.Shutdown()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	if _, err := hub.Adopt(New(ctrl, bc, config.ServeConfig{}), bc); err != nil {
		t.Fatal(err)
	}

	direct := httptest.NewServer(hub.Handler())
	defer direct.Close()
	if status, code := postLaunchJSON(t, direct.URL+"/approve", `{"id":"1","allow":true}`, nil); status != http.StatusForbidden || code != codeLaunchTokenRequired {
		t.Fatalf("hub POST /approve off a bare socket = %d %q, want 403 %q", status, code, codeLaunchTokenRequired)
	}

	resp, err := hub.InProcessClient().Post("http://kernel/approve", "application/json", strings.NewReader(`{"allow":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("in-process POST /approve = %d, want the handler's 400", resp.StatusCode)
	}

	ln, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	origin := LoopbackOrigin(ln)
	gated := httptest.NewUnstartedServer(NewLoopbackGate(hub.Handler(), LoopbackGateOptions{Token: "launch-credential", Origin: origin}))
	_ = gated.Listener.Close()
	gated.Listener = ln
	gated.Start()
	defer gated.Close()
	status, _ := postLaunchJSON(t, origin+"/approve", `{"allow":true}`, http.Header{
		"Origin": {origin},
		"Cookie": {TokenCookie + "=launch-credential"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("loopback-gated POST /approve = %d, want the handler's 400", status)
	}
}
