package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func accountServer(t *testing.T, granted bool) *httptest.Server {
	t.Helper()
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	if granted {
		s.AllowAccountAuth()
	}
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)
	return srv
}

// Signing in writes a token into the credential store of the machine running
// the kernel. A server on the network must not let a client do that, so the
// routes stay shut until a host with one local window asks for them.
func TestAccountRoutesRefusedWithoutHostGrant(t *testing.T) {
	srv := accountServer(t, false)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/account", ""},
		{http.MethodPost, "/account/login", ""},
		{http.MethodPost, "/account/poll", `{"deviceCode":"x"}`},
		{http.MethodPost, "/account/logout", ""},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		// The CSRF guard runs ahead of the handler and rejects a POST without
		// it; the grant check is what this test is actually after.
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403 without the host grant", tc.method, tc.path, resp.StatusCode)
		}
	}
}

// Signed out is an answer, not a failure: the panel renders from it.
func TestAccountReportsSignedOut(t *testing.T) {
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "")
	srv := accountServer(t, true)
	resp, err := srv.Client().Get(srv.URL + "/account")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /account = %d, want 200", resp.StatusCode)
	}
	var body struct {
		SignedIn bool `json:"signedIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.SignedIn {
		t.Error("no stored token must read as signed out")
	}
}

func TestAccountPollRequiresADeviceCode(t *testing.T) {
	srv := accountServer(t, true)
	resp, err := srv.Client().Post(srv.URL+"/account/poll", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("poll without a device code = %d, want 400", resp.StatusCode)
	}
}
