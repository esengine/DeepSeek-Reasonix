package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOAuthServer stands up a complete simulated MCP authorization server with
// protected-resource discovery, AS metadata, dynamic registration, and a token
// endpoint. It records the calls it receives.
type fakeOAuthServer struct {
	srv *httptest.Server

	mu            sync.Mutex
	tokenHits     int
	registerHits  int
	lastCode      string
	lastGrantType string
	lastVerifier  string
	lastRedirect  string
	// tokenResponse overrides the default token response body.
	tokenResponse map[string]any
}

func newFakeOAuthServer(t *testing.T) *fakeOAuthServer {
	t.Helper()
	f := &fakeOAuthServer{tokenResponse: map[string]any{
		"access_token":  "ACCESS_123",
		"refresh_token": "REFRESH_456",
		"token_type":    "Bearer",
		"expires_in":    3600,
	}}
	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, ProtectedResourceMetadata{Resource: srv.URL, AuthorizationServers: []string{srv.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, AuthorizationServerMetadata{
			Issuer:                        srv.URL,
			AuthorizationEndpoint:         srv.URL + "/authorize",
			TokenEndpoint:                 srv.URL + "/token",
			RegistrationEndpoint:          srv.URL + "/register",
			CodeChallengeMethodsSupported: []string{"S256"},
			GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.registerHits++
		f.mu.Unlock()
		writeJSON(t, w, map[string]any{"client_id": "dyn-client-1"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenHits++
		f.lastGrantType = r.FormValue("grant_type")
		f.lastCode = r.FormValue("code")
		f.lastVerifier = r.FormValue("code_verifier")
		f.lastRedirect = r.FormValue("redirect_uri")
		f.mu.Unlock()
		writeJSON(t, w, f.tokenResponse)
	})

	srv = httptest.NewServer(mux)
	f.srv = srv
	t.Cleanup(srv.Close)
	return f
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// completingOpenURL returns an OpenURL func that simulates the browser by
// calling the redirect_uri with the parsed code and state. It posts back the
// given authorization code.
func completingOpenURL(t *testing.T, code string) func(string) error {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		redirect := q.Get("redirect_uri")
		state := q.Get("state")
		if redirect == "" || state == "" {
			t.Fatalf("auth URL missing redirect_uri/state: %s", authURL)
		}
		cb := redirect + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, err := http.Get(cb)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	}
}

func TestAuthorizeFullFlow(t *testing.T) {
	f := newFakeOAuthServer(t)
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		OpenURL:   completingOpenURL(t, "AUTH_CODE_1"),
	})

	token, err := c.Authorize(context.Background(), "demo", f.srv.URL)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if token != "Bearer ACCESS_123" {
		t.Fatalf("token = %q, want %q", token, "Bearer ACCESS_123")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.registerHits != 1 {
		t.Fatalf("expected 1 DCR call, got %d", f.registerHits)
	}
	if f.lastGrantType != "authorization_code" {
		t.Fatalf("token grant_type = %q, want authorization_code", f.lastGrantType)
	}
	if f.lastCode != "AUTH_CODE_1" {
		t.Fatalf("token code = %q, want AUTH_CODE_1", f.lastCode)
	}
	if strings.TrimSpace(f.lastVerifier) == "" {
		t.Fatal("token exchange must send a code_verifier (PKCE)")
	}
}

func TestAuthorizeReusesCachedToken(t *testing.T) {
	f := newFakeOAuthServer(t)
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		OpenURL:   completingOpenURL(t, "CODE"),
	})

	if _, err := c.Authorize(context.Background(), "demo", f.srv.URL); err != nil {
		t.Fatalf("first Authorize: %v", err)
	}
	f.mu.Lock()
	first := f.tokenHits
	f.mu.Unlock()

	// A second Authorize immediately after must not hit the network again.
	token, err := c.Authorize(context.Background(), "demo", f.srv.URL)
	if err != nil {
		t.Fatalf("second Authorize: %v", err)
	}
	if token != "Bearer ACCESS_123" {
		t.Fatalf("token = %q", token)
	}
	f.mu.Lock()
	if f.tokenHits != first {
		t.Fatalf("cached token should not re-hit token endpoint: %d -> %d", first, f.tokenHits)
	}
	f.mu.Unlock()
}

func TestCurrentTokenReturnsCachedWithoutFlow(t *testing.T) {
	f := newFakeOAuthServer(t)
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		OpenURL:   completingOpenURL(t, "CODE"),
	})
	if _, err := c.Authorize(context.Background(), "demo", f.srv.URL); err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	got := c.CurrentToken(f.srv.URL)
	if got != "Bearer ACCESS_123" {
		t.Fatalf("CurrentToken = %q, want Bearer ACCESS_123", got)
	}
}

func TestCurrentTokenRefreshesExpiredToken(t *testing.T) {
	f := newFakeOAuthServer(t)
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		OpenURL:   completingOpenURL(t, "CODE"),
	})
	// Seed an expired token with a refresh token and the discovered metadata.
	origin := serverOrigin(f.srv.URL)
	as := &AuthorizationServerMetadata{Issuer: f.srv.URL, TokenEndpoint: f.srv.URL + "/token"}
	if err := c.store.Put(origin, &StoredCredential{
		Token:  Token{AccessToken: "OLD", RefreshToken: "REFRESH_456", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour)},
		Server: &ServerMetadata{ResourceURL: origin, AuthServer: as},
	}); err != nil {
		t.Fatalf("seed Put: %v", err)
	}

	// CurrentToken must refresh non-interactively.
	got := c.CurrentToken(f.srv.URL)
	if got != "Bearer ACCESS_123" {
		t.Fatalf("CurrentToken = %q, want refreshed Bearer ACCESS_123", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastGrantType != "refresh_token" {
		t.Fatalf("expected refresh_token grant, got %q", f.lastGrantType)
	}
}

func TestAuthorizeRejectsStateMismatch(t *testing.T) {
	f := newFakeOAuthServer(t)
	// Simulate a tampered redirect: the state echoed back does not match.
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		OpenURL: func(authURL string) error {
			u, _ := url.Parse(authURL)
			redirect := u.Query().Get("redirect_uri")
			// wrong state
			cb := redirect + "?code=X&state=ATTACKER_STATE"
			resp, err := http.Get(cb)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		},
	})
	if _, err := c.Authorize(context.Background(), "demo", f.srv.URL); err == nil {
		t.Fatal("expected error for state mismatch")
	}
}

func TestAuthorizeRequiresHttpOrigin(t *testing.T) {
	c := newTestClient(t, Options{})
	if _, err := c.Authorize(context.Background(), "demo", "not-a-url"); err == nil {
		t.Fatal("expected error for non-http origin")
	}
}
