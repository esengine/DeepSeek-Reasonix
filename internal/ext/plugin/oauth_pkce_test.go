package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// pkceSilentServer is an authorization server whose metadata never says which
// PKCE methods it supports.
func pkceSilentServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": server.URL + "/mcp", "authorization_servers": []string{server.URL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"registration_endpoint":  server.URL + "/register",
			})
		case "/register":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "c-1"})
		default:
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+server.URL+`/.well-known/oauth-protected-resource"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

var errStopAtBrowser = errors.New("test stops at the browser")

// A server that does not advertise PKCE may not verify the challenge it is
// sent, and the spec has the client refuse it, by identity and before any
// browser opens. The user's own config can let it through; PKCE is still sent.
func TestOAuthRefusesAnAuthorizationServerThatDoesNotAdvertisePKCE(t *testing.T) {
	server := pkceSilentServer(t)
	for _, allow := range []bool{false, true} {
		opened := ""
		spec := Spec{Name: "silent", Type: "http", URL: server.URL + "/mcp", StateDir: testenv.TempDir(t), OAuthAllowMissingPKCEMetadata: allow}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := AuthorizeHTTPMCP(ctx, spec, func(raw string) error {
			opened = raw
			return errStopAtBrowser
		})
		cancel()
		if !allow {
			if !errors.Is(err, ErrOAuthPKCEUnadvertised) || opened != "" {
				t.Fatalf("strict: err = %v, browser opened %q; want ErrOAuthPKCEUnadvertised before any browser", err, opened)
			}
			continue
		}
		if errors.Is(err, ErrOAuthPKCEUnadvertised) || opened == "" {
			t.Fatalf("allowed: err = %v, browser opened %q; want the flow to reach the browser", err, opened)
		}
		authURL, _ := url.Parse(opened)
		if authURL.Query().Get("code_challenge_method") != "S256" || authURL.Query().Get("code_challenge") == "" {
			t.Fatalf("allowed: authorization URL %q carries no S256 challenge", opened)
		}
	}
}
