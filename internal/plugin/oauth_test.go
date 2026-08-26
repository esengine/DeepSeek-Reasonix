package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseBearerChallenge(t *testing.T) {
	metadata, scope, ok := parseBearerChallenge(`Basic realm="legacy", Bearer resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource", scope="mcp:connect files:read"`)
	if !ok {
		t.Fatal("Bearer challenge was not parsed")
	}
	if metadata != "https://mcp.example.test/.well-known/oauth-protected-resource" {
		t.Fatalf("resource metadata = %q", metadata)
	}
	if scope != "mcp:connect files:read" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestPKCEChallengeMatchesRFC7636KnownAnswer(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallenge(verifier); got != want {
		t.Fatalf("PKCE challenge = %q, want %q", got, want)
	}
}

func TestOAuthHTTPClientDoesNotChangeRuntimeIdentity(t *testing.T) {
	base := Spec{Name: "remote", Type: "http", URL: "https://mcp.example.test"}
	withClient := base
	withClient.OAuthHTTPClient = &http.Client{}
	if !MCPRuntimeSpecMatches(base, withClient) {
		t.Fatal("host-local OAuth HTTP client changed MCP runtime identity")
	}
}

func TestAuthorizeHTTPMCPRejectsStaticAuthorizationHeader(t *testing.T) {
	opened := false
	err := AuthorizeHTTPMCP(context.Background(), Spec{
		Name: "remote", Type: "http", URL: "https://example.test/mcp", StateDir: t.TempDir(),
		Headers: map[string]string{"Authorization": "Bearer configured"},
	}, func(string) error {
		opened = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "explicit authentication") {
		t.Fatalf("AuthorizeHTTPMCP error = %v", err)
	}
	if opened {
		t.Fatal("static Authorization configuration opened the OAuth browser")
	}
}

func TestAuthorizeHTTPMCPRejectsStaticAPIKeyHeader(t *testing.T) {
	opened := false
	err := AuthorizeHTTPMCP(context.Background(), Spec{
		Name: "remote", Type: "http", URL: "https://example.test/mcp", StateDir: t.TempDir(),
		Headers: map[string]string{"X-API-Key": "configured"},
	}, func(string) error {
		opened = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "explicit authentication") {
		t.Fatalf("AuthorizeHTTPMCP error = %v", err)
	}
	if opened {
		t.Fatal("static API key configuration opened the OAuth browser")
	}
}

func TestAuthorizeHTTPMCPUsesDiscoveryPKCEAndPersistsPrivateToken(t *testing.T) {
	stateDir := t.TempDir()
	var server *httptest.Server
	var mu sync.Mutex
	registeredRedirect := ""
	codeChallenge := ""
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer access-one" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q, scope="mcp:connect"`, server.URL+"/.well-known/oauth-protected-resource"))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeOAuthMCPFixtureResponse(w, r)
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL},
				"scopes_supported":      []string{"mcp:connect"},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                server.URL,
				"authorization_endpoint":                server.URL + "/authorize",
				"token_endpoint":                        server.URL + "/token",
				"registration_endpoint":                 server.URL + "/register",
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
			})
		case "/register":
			var registration map[string]any
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				t.Errorf("decode registration: %v", err)
				http.Error(w, "bad registration", http.StatusBadRequest)
				return
			}
			redirects, _ := registration["redirect_uris"].([]any)
			if len(redirects) != 1 {
				t.Errorf("redirect_uris = %#v", registration["redirect_uris"])
			} else {
				registeredRedirect, _ = redirects[0].(string)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"client_id":                  "reasonix-test",
				"client_secret":              "client-secret",
				"token_endpoint_auth_method": "client_secret_basic",
			})
		case "/token":
			if user, pass, ok := r.BasicAuth(); !ok || user != "reasonix-test" || pass != "client-secret" {
				t.Errorf("token endpoint client authentication = (%q, %q, %v)", user, pass, ok)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			verifier := r.Form.Get("code_verifier")
			mu.Lock()
			expectedChallenge := codeChallenge
			mu.Unlock()
			if verifier == "" || pkceChallenge(verifier) != expectedChallenge {
				t.Errorf("PKCE verifier does not match challenge")
			}
			if got := r.Form.Get("resource"); got != server.URL+"/mcp" {
				t.Errorf("token resource = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-one",
				"refresh_token": "refresh-one",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "mcp:connect",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var oauthRequests atomic.Int32
	spec := Spec{
		Name: "figma", Type: "http", URL: server.URL + "/mcp", StateDir: stateDir,
		OAuthHTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			oauthRequests.Add(1)
			return http.DefaultTransport.RoundTrip(req)
		})},
	}
	openURL := func(raw string) error {
		authURL, err := url.Parse(raw)
		if err != nil {
			return err
		}
		if authURL.Path != "/authorize" {
			return fmt.Errorf("authorization path = %q", authURL.Path)
		}
		query := authURL.Query()
		if query.Get("code_challenge_method") != "S256" {
			return fmt.Errorf("code challenge method = %q", query.Get("code_challenge_method"))
		}
		if query.Get("resource") != server.URL+"/mcp" {
			return fmt.Errorf("authorization resource = %q", query.Get("resource"))
		}
		mu.Lock()
		codeChallenge = query.Get("code_challenge")
		mu.Unlock()
		callback, err := url.Parse(query.Get("redirect_uri"))
		if err != nil {
			return err
		}
		values := callback.Query()
		values.Set("code", "authorization-code")
		values.Set("state", query.Get("state"))
		callback.RawQuery = values.Encode()
		go func() {
			resp, err := http.Get(callback.String())
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := AuthorizeHTTPMCP(ctx, spec, openURL); err != nil {
		t.Fatalf("AuthorizeHTTPMCP: %v", err)
	}
	if oauthRequests.Load() == 0 {
		t.Fatal("AuthorizeHTTPMCP did not use the injected proxy-aware HTTP client")
	}
	if !strings.HasPrefix(registeredRedirect, "http://127.0.0.1:") {
		t.Fatalf("registered redirect = %q", registeredRedirect)
	}
	tokenPath := filepath.Join(stateDir, mcpOAuthStateFile)
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat token state: %v", err)
	}
	// Windows has no Unix permission bits: os.WriteFile's 0600 intent is
	// unobservable there (mode reports 0666), so the permission contract is
	// asserted only where it exists.
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("token state mode = %o, want 600", got)
		}
	}

	transport, err := newHTTPTransport(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	result, err := transport.call(context.Background(), "ping", map[string]any{})
	if err != nil {
		t.Fatalf("authenticated MCP call: %v", err)
	}
	if string(result) != `{}` {
		t.Fatalf("result = %s, want typed empty ping result", result)
	}
}

func TestAuthorizeHTTPMCPDoesNotHoldStateLockDuringBrowser(t *testing.T) {
	stateDir := t.TempDir()
	const endpoint = "https://mcp.example.test/mcp"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
		switch req.URL.Path {
		case "/mcp":
			resp, err := response(http.StatusUnauthorized, `unauthorized`)
			if err != nil {
				return nil, err
			}
			resp.Header.Set("WWW-Authenticate", `Bearer resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource"`)
			return resp, nil
		case "/.well-known/oauth-protected-resource":
			return response(http.StatusOK, `{"resource":"https://mcp.example.test/mcp","authorization_servers":["https://mcp.example.test"],"scopes_supported":["mcp:connect"]}`)
		case "/.well-known/oauth-authorization-server":
			return response(http.StatusOK, `{"issuer":"https://mcp.example.test","authorization_endpoint":"https://mcp.example.test/authorize","token_endpoint":"https://mcp.example.test/token","registration_endpoint":"https://mcp.example.test/register","code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["client_secret_basic"]}`)
		case "/register":
			return response(http.StatusOK, `{"client_id":"reasonix-test","client_secret":"client-secret","token_endpoint_auth_method":"client_secret_basic"}`)
		case "/token":
			return response(http.StatusOK, `{"access_token":"access-one","refresh_token":"refresh-one","token_type":"Bearer","expires_in":3600}`)
		default:
			return response(http.StatusNotFound, `not found`)
		}
	})}

	openURL := func(raw string) error {
		authURL, err := url.Parse(raw)
		if err != nil {
			return err
		}
		clearDone := make(chan error, 1)
		go func() {
			_, clearErr := ClearHTTPMCPOAuth(Spec{StateDir: stateDir})
			clearDone <- clearErr
		}()
		select {
		case clearErr := <-clearDone:
			if clearErr != nil {
				return fmt.Errorf("clear during browser flow: %w", clearErr)
			}
		case <-time.After(time.Second):
			return fmt.Errorf("clear during browser flow blocked on OAuth state lock")
		}
		callback, err := url.Parse(authURL.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		query := callback.Query()
		query.Set("code", "authorization-code")
		query.Set("state", authURL.Query().Get("state"))
		callback.RawQuery = query.Encode()
		resp, err := http.Get(callback.String())
		if err == nil {
			_ = resp.Body.Close()
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := AuthorizeHTTPMCP(ctx, Spec{
		Name: "remote", Type: "http", URL: endpoint, StateDir: stateDir,
		OAuthHTTPClient: client,
	}, openURL)
	if err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("AuthorizeHTTPMCP after concurrent clear = %v, want invalidation", err)
	}
	if _, err := os.Stat(mcpOAuthStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OAuth state was written after concurrent clear: %v", err)
	}
}

func TestHTTPMCPRefreshesExpiredTokenAndRotatesRefreshToken(t *testing.T) {
	stateDir := t.TempDir()
	refreshCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			refreshCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh-old" {
				t.Errorf("refresh form = %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer access-new" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeOAuthMCPFixtureResponse(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := mcpOAuthState{
		Version: 1, Resource: server.URL + "/mcp", Issuer: server.URL,
		ClientID: "client", ClientSecret: "secret", TokenEndpoint: server.URL + "/token", TokenEndpointAuthMethod: "client_secret_basic",
		AccessToken: "access-old", RefreshToken: "refresh-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Minute),
	}
	if err := saveMCPOAuthState(stateDir, state); err != nil {
		t.Fatal(err)
	}
	transport, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: server.URL + "/mcp", StateDir: stateDir})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after refresh: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
	rotated, err := loadMCPOAuthState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken != "refresh-new" || rotated.AccessToken != "access-new" {
		t.Fatalf("rotated token state = %+v", rotated)
	}
}

func TestOAuthClientSecretBasicFormEncodesCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client+id%2B" || pass != "secret%3Avalue%2Fwith+space" {
			t.Errorf("OAuth Basic credentials = (%q, %q, %v)", user, pass, ok)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-new", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer server.Close()

	_, err := requestOAuthToken(context.Background(), server.Client(), mcpOAuthState{
		TokenEndpoint: server.URL, ClientID: "client id+", ClientSecret: "secret:value/with space", TokenEndpointAuthMethod: "client_secret_basic",
	}, url.Values{"grant_type": {"authorization_code"}})
	if err != nil {
		t.Fatalf("requestOAuthToken: %v", err)
	}
}

func TestHTTPMCPSerializesSharedRefreshTokenRotation(t *testing.T) {
	stateDir := t.TempDir()
	refreshStarted := make(chan struct{})
	allowRefresh := make(chan struct{})
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			call := refreshCalls.Add(1)
			if call != 1 {
				t.Errorf("refresh endpoint called %d times", call)
				http.Error(w, "duplicate refresh", http.StatusBadRequest)
				return
			}
			close(refreshStarted)
			<-allowRefresh
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse refresh form: %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "refresh-old" {
				t.Errorf("refresh token = %q, want refresh-old", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer access-new" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeOAuthMCPFixtureResponse(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: server.URL + "/mcp", Issuer: server.URL,
		ClientID: "client", ClientSecret: "secret", TokenEndpoint: server.URL + "/token", TokenEndpointAuthMethod: "client_secret_basic",
		AccessToken: "access-old", RefreshToken: "refresh-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Name: "remote", Type: "http", URL: server.URL + "/mcp", StateDir: stateDir}
	first, err := newHTTPTransport(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := newHTTPTransport(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()

	errs := make(chan error, 2)
	go func() {
		_, err := first.call(context.Background(), "ping", nil)
		errs <- err
	}()
	<-refreshStarted
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := second.call(context.Background(), "ping", nil)
		errs <- err
	}()
	<-secondStarted
	close(allowRefresh)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("shared refresh call: %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestMCPOAuthConcurrentUnauthorizedRefreshesUnexpiredTokenOnce(t *testing.T) {
	stateDir := t.TempDir()
	refreshStarted := make(chan struct{})
	allowRefresh := make(chan struct{})
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if call := refreshCalls.Add(1); call != 1 {
			t.Errorf("refresh endpoint called %d times", call)
		}
		if refreshCalls.Load() == 1 {
			close(refreshStarted)
			<-allowRefresh
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer server.Close()

	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: server.URL + "/mcp", Issuer: server.URL,
		ClientID: "client", TokenEndpoint: server.URL + "/token",
		AccessToken: "access-revoked", RefreshToken: "refresh-old", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	client, err := newMCPOAuthClient(stateDir, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	authorize := func() error {
		request := httptest.NewRequest(http.MethodPost, server.URL+"/mcp", nil)
		request.Header.Set("Authorization", "Bearer access-revoked")
		response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}
		return client.Authorize(t.Context(), request, response)
	}
	errs := make(chan error, 2)
	go func() { errs <- authorize() }()
	<-refreshStarted
	go func() { errs <- authorize() }()
	close(allowRefresh)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Authorize: %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want one shared refresh", got)
	}
	if client.state.AccessToken != "access-new" {
		t.Fatalf("OAuth client kept stale access token %q", client.state.AccessToken)
	}
}

func TestHTTPMCPRefreshReleasesCrossProcessLockDuringTokenRequest(t *testing.T) {
	stateDir := t.TempDir()
	refreshStarted := make(chan struct{})
	allowRefresh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		close(refreshStarted)
		<-allowRefresh
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer server.Close()
	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: server.URL + "/mcp", TokenEndpoint: server.URL + "/token", ClientID: "client",
		AccessToken: "access-old", RefreshToken: "refresh-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	transport, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: server.URL + "/mcp", StateDir: stateDir})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	callDone := make(chan error, 1)
	go func() {
		_, callErr := transport.call(context.Background(), "ping", nil)
		callDone <- callErr
	}()
	<-refreshStarted

	clearDone := make(chan error, 1)
	go func() {
		_, clearErr := ClearHTTPMCPOAuth(Spec{StateDir: stateDir})
		clearDone <- clearErr
	}()
	select {
	case clearErr := <-clearDone:
		if clearErr != nil {
			t.Fatalf("ClearHTTPMCPOAuth during refresh: %v", clearErr)
		}
	case <-time.After(time.Second):
		t.Fatal("ClearHTTPMCPOAuth blocked on the token endpoint")
	}
	close(allowRefresh)
	if err := <-callDone; err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("refresh after clear error = %v, want invalidation", err)
	}
	if _, err := os.Stat(mcpOAuthStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleared OAuth state was recreated: %v", err)
	}
}

func TestHTTPMCPRejectsOAuthStateForDifferentResource(t *testing.T) {
	stateDir := t.TempDir()
	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: "https://old.example.test/mcp", Issuer: "https://auth.example.test",
		ClientID: "client", AccessToken: "must-not-leak", TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: "https://new.example.test/mcp", StateDir: stateDir})
	if err == nil || !strings.Contains(err.Error(), "different MCP resource") {
		t.Fatalf("newHTTPTransport error = %v, want resource-binding rejection", err)
	}
}

func TestSameCanonicalResourceRejectsURLUserinfo(t *testing.T) {
	if sameCanonicalResource("https://user:pass@mcp.example.test/mcp", "https://mcp.example.test/mcp") {
		t.Fatal("credentialed URL must not match an OAuth resource")
	}
}

func TestClearHTTPMCPOAuthRemovesOnlyReasonixState(t *testing.T) {
	stateDir := t.TempDir()
	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: "https://mcp.example.test/mcp", Issuer: "https://auth.example.test",
		ClientID: "client", AccessToken: "access-token", TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(stateDir, "session.json")
	if err := os.WriteFile(neighbor, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := ClearHTTPMCPOAuth(Spec{StateDir: stateDir})
	if err != nil {
		t.Fatalf("ClearHTTPMCPOAuth: %v", err)
	}
	if !changed {
		t.Fatal("ClearHTTPMCPOAuth reported no change")
	}
	if _, err := os.Stat(filepath.Join(stateDir, mcpOAuthStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OAuth state still exists or stat failed: %v", err)
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got) != "keep" {
		t.Fatalf("neighboring MCP state changed: data=%q err=%v", got, err)
	}
	changed, err = ClearHTTPMCPOAuth(Spec{StateDir: stateDir})
	if err != nil || changed {
		t.Fatalf("second ClearHTTPMCPOAuth = (%v, %v), want (false, nil)", changed, err)
	}
}

func TestClearHTTPMCPOAuthAllowsMissingPrivateStateDirectory(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "not-created-yet")
	changed, err := ClearHTTPMCPOAuth(Spec{StateDir: stateDir})
	if err != nil || changed {
		t.Fatalf("ClearHTTPMCPOAuth = (%v, %v), want (false, nil)", changed, err)
	}
}

func TestReconcileHTTPMCPOAuthAfterRemovalPreservesOnlyMatchingFallback(t *testing.T) {
	stateDir := t.TempDir()
	const resource = "https://mcp.example.test/mcp?workspace=main"
	writeState := func() {
		t.Helper()
		if err := saveMCPOAuthState(stateDir, mcpOAuthState{
			Version: 1, Resource: resource, ClientID: "client", AccessToken: "access", TokenType: "Bearer",
		}); err != nil {
			t.Fatal(err)
		}
	}
	writeState()
	changed, err := ReconcileHTTPMCPOAuthAfterRemoval(Spec{StateDir: stateDir}, resource)
	if err != nil || changed {
		t.Fatalf("matching fallback reconciliation = (%v, %v), want (false, nil)", changed, err)
	}
	if _, err := os.Stat(mcpOAuthStatePath(stateDir)); err != nil {
		t.Fatalf("matching fallback OAuth state was removed: %v", err)
	}

	changed, err = ReconcileHTTPMCPOAuthAfterRemoval(Spec{StateDir: stateDir}, "https://other.example.test/mcp")
	if err != nil || !changed {
		t.Fatalf("different fallback reconciliation = (%v, %v), want (true, nil)", changed, err)
	}
	if _, err := os.Stat(mcpOAuthStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("different fallback OAuth state still exists: %v", err)
	}
}

func TestMCPAuthGenerationInvalidatesPendingAuthorization(t *testing.T) {
	stateDir := t.TempDir()
	generation, err := captureMCPOAuthGeneration(context.Background(), stateDir)
	if err != nil {
		t.Fatalf("captureMCPOAuthGeneration: %v", err)
	}
	if err := bumpMCPOAuthGeneration(stateDir); err != nil {
		t.Fatalf("bumpMCPOAuthGeneration: %v", err)
	}
	err = saveMCPOAuthStateIfGenerationUnchanged(context.Background(), stateDir, generation, mcpOAuthState{
		Resource: "https://mcp.example.test/mcp", AccessToken: "must-not-save",
	})
	if err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("save after invalidation error = %v", err)
	}
	if _, err := os.Stat(mcpOAuthStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalidated authorization wrote OAuth state: %v", err)
	}
}

func TestReconcileDifferentFallbackInvalidatesPendingAuthorizationWithoutState(t *testing.T) {
	stateDir := t.TempDir()
	generation, err := captureMCPOAuthGeneration(context.Background(), stateDir)
	if err != nil {
		t.Fatalf("captureMCPOAuthGeneration: %v", err)
	}
	changed, err := ReconcileHTTPMCPOAuthAfterRemoval(Spec{StateDir: stateDir}, "https://other.example.test/mcp")
	if err != nil || changed {
		t.Fatalf("reconcile without OAuth state = (%v, %v), want (false, nil)", changed, err)
	}
	err = saveMCPOAuthStateIfGenerationUnchanged(context.Background(), stateDir, generation, mcpOAuthState{
		Resource: "https://removed.example.test/mcp", AccessToken: "must-not-save",
	})
	if err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("save after different fallback reconciliation error = %v", err)
	}
}

func TestClearedOAuthStateCannotBeResurrectedByStaleTransport(t *testing.T) {
	stateDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: server.URL, Issuer: server.URL, TokenEndpoint: server.URL + "/token",
		ClientID: "client", AccessToken: "access-old", RefreshToken: "refresh-old", TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	transport, err := newHTTPTransport(Spec{Name: "remote", Type: "http", URL: server.URL, StateDir: stateDir})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if changed, err := ClearHTTPMCPOAuth(Spec{StateDir: stateDir}); err != nil || !changed {
		t.Fatalf("ClearHTTPMCPOAuth = (%v, %v), want (true, nil)", changed, err)
	}
	if _, err := transport.call(context.Background(), "ping", nil); err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("stale transport call error = %v, want cleared-state failure", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, mcpOAuthStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale transport recreated OAuth state: %v", err)
	}
}

func TestOAuthErrorsRedactCredentialMaterial(t *testing.T) {
	const secret = "fixture-oauth-secret-do-not-log-123456"

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token","access_token":"` + secret + `"}`)),
	}
	if got := oauthHTTPError("token request", resp).Error(); strings.Contains(got, secret) {
		t.Fatalf("HTTP error leaked credential: %s", got)
	}

	result := make(chan oauthCallbackResult, 1)
	handler := oauthCallbackHandler("expected", result)
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=expected&error=access_denied&error_description=token%3A"+secret, nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if callback := <-result; callback.Err == nil || strings.Contains(callback.Err.Error(), secret) {
		t.Fatalf("callback error was not safely redacted: %v", callback.Err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
