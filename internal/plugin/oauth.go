package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"reasonix/internal/mcpdiag"
	"reasonix/internal/secrets"
)

const (
	mcpOAuthStateFile      = "oauth.json"
	mcpOAuthGenerationFile = "oauth.generation"
	maxOAuthBody           = 1 << 20
)

// Protocol sources: MCP Authorization (2025-11-25), RFC 9728 protected
// resources, RFC 8414 server metadata, RFC 7591 DCR, and RFC 7636 PKCE.

// mcpOAuthState is Reasonix-owned authorization state for one MCP server. It
// lives under Spec.StateDir, never in a project or another client's keychain.
// Versioned JSON gives future readers an explicit migration boundary.
type mcpOAuthState struct {
	Version                 int       `json:"version"`
	Resource                string    `json:"resource"`
	Issuer                  string    `json:"issuer"`
	AuthorizationEndpoint   string    `json:"authorization_endpoint"`
	TokenEndpoint           string    `json:"token_endpoint"`
	RegistrationEndpoint    string    `json:"registration_endpoint,omitempty"`
	ClientID                string    `json:"client_id"`
	ClientSecret            string    `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string    `json:"scope,omitempty"`
	AccessToken             string    `json:"access_token,omitempty"`
	RefreshToken            string    `json:"refresh_token,omitempty"`
	TokenType               string    `json:"token_type,omitempty"`
	Expiry                  time.Time `json:"expiry,omitempty"`
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

type dynamicClientRegistration struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

type mcpOAuthClient struct {
	stateDir string
	state    mcpOAuthState
	client   *http.Client
}

// AuthorizeHTTPMCP performs the user-initiated OAuth authorization-code flow
// for an HTTP MCP server. It implements protected-resource discovery, OAuth
// server metadata discovery, dynamic client registration, PKCE S256, a
// loopback callback, resource indicators, and private token persistence.
func AuthorizeHTTPMCP(ctx context.Context, spec Spec, openURL func(string) error) error {
	if err := validateHTTPMCPAuthorization(spec, openURL); err != nil {
		return err
	}
	if err := os.MkdirAll(spec.StateDir, 0o700); err != nil {
		return fmt.Errorf("MCP OAuth: prepare private state directory: %w", err)
	}
	generation, err := captureMCPOAuthGeneration(ctx, spec.StateDir)
	if err != nil {
		return err
	}
	endpoint, err := parseSecureOAuthURL(spec.URL, true)
	if err != nil {
		return fmt.Errorf("MCP OAuth endpoint: %w", err)
	}
	client := newOAuthHTTPClient(spec.OAuthHTTPClient)
	resourceMeta, challengedScope, err := discoverProtectedResource(ctx, client, endpoint)
	if err != nil {
		return err
	}
	resource, issuer, err := oauthResourceAndIssuer(resourceMeta, endpoint)
	if err != nil {
		return err
	}
	authMeta, err := discoverAuthorizationServer(ctx, client, issuer)
	if err != nil {
		return err
	}
	if strings.TrimSpace(authMeta.AuthorizationEndpoint) == "" || strings.TrimSpace(authMeta.TokenEndpoint) == "" {
		return fmt.Errorf("MCP OAuth: authorization server metadata is missing authorization_endpoint or token_endpoint")
	}
	if len(authMeta.CodeChallengeMethodsSupported) > 0 && !slices.Contains(authMeta.CodeChallengeMethodsSupported, "S256") {
		return fmt.Errorf("MCP OAuth: authorization server does not support PKCE S256")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("MCP OAuth callback: %w", err)
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + "/oauth/callback"

	registration, err := registerOAuthClient(ctx, client, authMeta, redirectURI)
	if err != nil {
		return err
	}
	verifier, err := randomBase64URL(64)
	if err != nil {
		return fmt.Errorf("MCP OAuth PKCE: %w", err)
	}
	requestState, err := randomBase64URL(32)
	if err != nil {
		return fmt.Errorf("MCP OAuth state: %w", err)
	}
	scope := strings.TrimSpace(challengedScope)
	if scope == "" {
		scope = strings.Join(resourceMeta.ScopesSupported, " ")
	}

	callbackResult := make(chan oauthCallbackResult, 1)
	callbackServer := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	callbackServer.Handler = oauthCallbackHandler(requestState, callbackResult)
	serveDone := make(chan error, 1)
	go func() {
		err := callbackServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()
	defer callbackServer.Close()

	authorizationURL, err := buildAuthorizationURL(authMeta.AuthorizationEndpoint, registration.ClientID, redirectURI, requestState, verifier, resource, scope)
	if err != nil {
		return err
	}
	if err := openURL(authorizationURL); err != nil {
		return fmt.Errorf("open MCP authorization page: %w", err)
	}

	var callback oauthCallbackResult
	select {
	case <-ctx.Done():
		return fmt.Errorf("MCP OAuth callback: %w", ctx.Err())
	case err := <-serveDone:
		if err != nil {
			return fmt.Errorf("MCP OAuth callback server: %w", err)
		}
		return fmt.Errorf("MCP OAuth callback server stopped before authorization completed")
	case callback = <-callbackResult:
	}
	if callback.Err != nil {
		return callback.Err
	}

	state := mcpOAuthState{
		Version:                 1,
		Resource:                resource,
		Issuer:                  authMeta.Issuer,
		AuthorizationEndpoint:   authMeta.AuthorizationEndpoint,
		TokenEndpoint:           authMeta.TokenEndpoint,
		RegistrationEndpoint:    authMeta.RegistrationEndpoint,
		ClientID:                registration.ClientID,
		ClientSecret:            registration.ClientSecret,
		TokenEndpointAuthMethod: registration.TokenEndpointAuthMethod,
		Scope:                   scope,
	}
	token, err := exchangeAuthorizationCode(ctx, client, state, callback.Code, verifier, redirectURI)
	if err != nil {
		return err
	}
	applyTokenResponse(&state, token, time.Now())
	return saveMCPOAuthStateIfGenerationUnchanged(ctx, spec.StateDir, generation, state)
}

func validateHTTPMCPAuthorization(spec Spec, openURL func(string) error) error {
	if openURL == nil {
		return fmt.Errorf("MCP OAuth: browser opener is required")
	}
	if !isHTTPMCPTransport(spec.Type) {
		return fmt.Errorf("MCP OAuth is only available for HTTP transports")
	}
	if mcpdiag.HasAuthConfig(spec.Headers, spec.Env, spec.URL) {
		return fmt.Errorf("MCP OAuth is unavailable while explicit authentication is configured")
	}
	if strings.TrimSpace(spec.StateDir) == "" {
		return fmt.Errorf("MCP OAuth: private state directory is unavailable")
	}
	return nil
}

// ClearHTTPMCPOAuth removes Reasonix-owned OAuth client and token state for one
// MCP server. It does not alter static headers or another application's data.
func ClearHTTPMCPOAuth(spec Spec) (bool, error) {
	return reconcileHTTPMCPOAuth(spec, "")
}

// ReconcileHTTPMCPOAuthAfterRemoval removes Reasonix-owned OAuth state after an
// MCP declaration is removed, unless the remaining effective HTTP declaration
// uses the same resource. Callers pass an empty remainingResource when no
// eligible fallback remains.
func ReconcileHTTPMCPOAuthAfterRemoval(spec Spec, remainingResource string) (bool, error) {
	return reconcileHTTPMCPOAuth(spec, strings.TrimSpace(remainingResource))
}

func reconcileHTTPMCPOAuth(spec Spec, remainingResource string) (bool, error) {
	path := mcpOAuthStatePath(spec.StateDir)
	if path == "" {
		return false, nil
	}
	if err := os.MkdirAll(spec.StateDir, 0o700); err != nil {
		return false, fmt.Errorf("clear MCP OAuth state: prepare private state directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := acquireMCPOAuthStateLock(ctx, spec.StateDir)
	if err != nil {
		return false, fmt.Errorf("clear MCP OAuth state: %w", err)
	}
	defer release()
	if remainingResource != "" {
		state, loadErr := loadMCPOAuthState(spec.StateDir)
		if loadErr == nil && sameCanonicalResource(state.Resource, remainingResource) {
			return false, nil
		}
	}
	if err := bumpMCPOAuthGeneration(spec.StateDir); err != nil {
		return false, fmt.Errorf("clear MCP OAuth state: %w", err)
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("clear MCP OAuth state: %w", err)
	}
	return true, nil
}

func newMCPOAuthClient(stateDir string, httpClient *http.Client) (*mcpOAuthClient, error) {
	state, err := loadMCPOAuthState(stateDir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(state.AccessToken) == "" && strings.TrimSpace(state.RefreshToken) == "" {
		return nil, nil
	}
	return &mcpOAuthClient{stateDir: stateDir, state: state, client: newOAuthHTTPClient(httpClient)}, nil
}

func (c *mcpOAuthClient) authorizationHeader(ctx context.Context, forceRefresh bool) (string, bool, error) {
	if c == nil {
		return "", false, nil
	}
	needsRefresh := forceRefresh || (strings.TrimSpace(c.state.RefreshToken) != "" && !c.state.Expiry.IsZero() && time.Now().Add(30*time.Second).After(c.state.Expiry))
	if needsRefresh {
		if err := c.refresh(ctx); err != nil {
			return "", false, err
		}
	}
	if strings.TrimSpace(c.state.AccessToken) == "" {
		return "", false, nil
	}
	tokenType := strings.TrimSpace(c.state.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if !strings.EqualFold(tokenType, "Bearer") {
		return "", false, fmt.Errorf("MCP OAuth: unsupported token type %q", tokenType)
	}
	return "Bearer " + c.state.AccessToken, true, nil
}

func (c *mcpOAuthClient) canRefresh() bool {
	return c != nil && strings.TrimSpace(c.state.RefreshToken) != "" && strings.TrimSpace(c.state.TokenEndpoint) != ""
}

func discoverProtectedResource(ctx context.Context, client *http.Client, endpoint *url.URL) (protectedResourceMetadata, string, error) {
	var metadataURL string
	var scope string
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		return protectedResourceMetadata{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			for _, challenge := range resp.Header.Values("WWW-Authenticate") {
				if foundURL, foundScope, ok := parseBearerChallenge(challenge); ok {
					metadataURL, scope = foundURL, foundScope
					break
				}
			}
		}
	}
	if metadataURL != "" {
		u, parseErr := parseSecureOAuthURL(metadataURL, true)
		if parseErr != nil {
			return protectedResourceMetadata{}, "", fmt.Errorf("MCP OAuth resource metadata URL: %w", parseErr)
		}
		if !sameHTTPOrigin(endpoint, u) {
			return protectedResourceMetadata{}, "", fmt.Errorf("MCP OAuth resource metadata URL changed origin")
		}
		var metadata protectedResourceMetadata
		if err := getOAuthJSON(ctx, client, u.String(), &metadata); err != nil {
			return protectedResourceMetadata{}, "", fmt.Errorf("MCP OAuth protected resource metadata: %w", err)
		}
		return metadata, scope, nil
	}

	var lastErr error
	for _, candidate := range protectedResourceMetadataURLs(endpoint) {
		var metadata protectedResourceMetadata
		if err := getOAuthJSON(ctx, client, candidate, &metadata); err == nil {
			return metadata, scope, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil && err != nil {
		lastErr = err
	}
	return protectedResourceMetadata{}, "", fmt.Errorf("MCP OAuth protected resource discovery failed: %w", lastErr)
}

func discoverAuthorizationServer(ctx context.Context, client *http.Client, issuer *url.URL) (authorizationServerMetadata, error) {
	var lastErr error
	for _, candidate := range authorizationServerMetadataURLs(issuer) {
		var metadata authorizationServerMetadata
		if err := getOAuthJSON(ctx, client, candidate, &metadata); err != nil {
			lastErr = err
			continue
		}
		if strings.TrimRight(metadata.Issuer, "/") != strings.TrimRight(issuer.String(), "/") {
			lastErr = fmt.Errorf("issuer mismatch: metadata=%q requested=%q", metadata.Issuer, issuer.String())
			continue
		}
		if _, err := parseSecureOAuthURL(metadata.AuthorizationEndpoint, true); err != nil {
			return authorizationServerMetadata{}, fmt.Errorf("MCP OAuth authorization endpoint: %w", err)
		}
		if _, err := parseSecureOAuthURL(metadata.TokenEndpoint, true); err != nil {
			return authorizationServerMetadata{}, fmt.Errorf("MCP OAuth token endpoint: %w", err)
		}
		return metadata, nil
	}
	return authorizationServerMetadata{}, fmt.Errorf("MCP OAuth authorization server discovery failed: %w", lastErr)
}

func registerOAuthClient(ctx context.Context, client *http.Client, metadata authorizationServerMetadata, redirectURI string) (dynamicClientRegistration, error) {
	if strings.TrimSpace(metadata.RegistrationEndpoint) == "" {
		return dynamicClientRegistration{}, fmt.Errorf("MCP OAuth: authorization server does not advertise dynamic client registration")
	}
	if _, err := parseSecureOAuthURL(metadata.RegistrationEndpoint, true); err != nil {
		return dynamicClientRegistration{}, fmt.Errorf("MCP OAuth registration endpoint: %w", err)
	}
	method := chooseTokenEndpointAuthMethod(metadata.TokenEndpointAuthMethodsSupported)
	body, err := json.Marshal(map[string]any{
		"client_name":                "Reasonix",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
	})
	if err != nil {
		return dynamicClientRegistration{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return dynamicClientRegistration{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return dynamicClientRegistration{}, fmt.Errorf("MCP OAuth client registration: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return dynamicClientRegistration{}, oauthHTTPError("MCP OAuth client registration", resp)
	}
	var registration dynamicClientRegistration
	if err := decodeLimitedJSON(resp.Body, &registration); err != nil {
		return dynamicClientRegistration{}, fmt.Errorf("MCP OAuth client registration: %w", err)
	}
	if strings.TrimSpace(registration.ClientID) == "" {
		return dynamicClientRegistration{}, fmt.Errorf("MCP OAuth client registration returned no client_id")
	}
	if registration.TokenEndpointAuthMethod == "" {
		registration.TokenEndpointAuthMethod = method
	}
	return registration, nil
}

func exchangeAuthorizationCode(ctx context.Context, client *http.Client, state mcpOAuthState, code, verifier, redirectURI string) (oauthTokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {state.ClientID},
		"resource":      {state.Resource},
	}
	return requestOAuthToken(ctx, client, state, form)
}

func requestOAuthToken(ctx context.Context, client *http.Client, state mcpOAuthState, form url.Values) (oauthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, state.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	switch state.TokenEndpointAuthMethod {
	case "client_secret_post":
		form.Set("client_secret", state.ClientSecret)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.ContentLength = int64(len(form.Encode()))
	case "none":
	default:
		if state.ClientSecret != "" {
			// RFC 6749 section 2.3.1 applies application/x-www-form-urlencoded
			// encoding to both credentials before constructing HTTP Basic auth.
			req.SetBasicAuth(url.QueryEscape(state.ClientID), url.QueryEscape(state.ClientSecret))
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return oauthTokenResponse{}, oauthHTTPError("MCP OAuth token request", resp)
	}
	var token oauthTokenResponse
	if err := decodeLimitedJSON(resp.Body, &token); err != nil {
		return oauthTokenResponse{}, err
	}
	if token.Error != "" {
		return oauthTokenResponse{}, fmt.Errorf("%s: %s", secrets.RedactCredentials(token.Error), secrets.RedactCredentials(token.Description))
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return oauthTokenResponse{}, fmt.Errorf("token response has no access_token")
	}
	return token, nil
}

func applyTokenResponse(state *mcpOAuthState, token oauthTokenResponse, now time.Time) {
	state.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		state.RefreshToken = token.RefreshToken
	}
	state.TokenType = token.TokenType
	if state.TokenType == "" {
		state.TokenType = "Bearer"
	}
	if token.Scope != "" {
		state.Scope = token.Scope
	}
	if token.ExpiresIn > 0 {
		state.Expiry = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	} else {
		state.Expiry = time.Time{}
	}
}

type oauthCallbackResult struct {
	Code string
	Err  error
}

func oauthCallbackHandler(expectedState string, result chan<- oauthCallbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/callback" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		var callback oauthCallbackResult
		switch {
		case query.Get("state") != expectedState:
			callback.Err = fmt.Errorf("MCP OAuth callback state did not match")
		case query.Get("error") != "":
			callback.Err = fmt.Errorf("MCP OAuth authorization failed: %s: %s", secrets.RedactCredentials(query.Get("error")), secrets.RedactCredentials(query.Get("error_description")))
		case strings.TrimSpace(query.Get("code")) == "":
			callback.Err = fmt.Errorf("MCP OAuth callback did not include an authorization code")
		default:
			callback.Code = query.Get("code")
		}
		if callback.Err != nil {
			http.Error(w, "Reasonix could not complete MCP authorization. You can close this window.", http.StatusBadRequest)
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><title>Reasonix MCP authorized</title><p>Authorization completed. You can close this window and return to Reasonix.</p>")
		}
		select {
		case result <- callback:
		default:
		}
	})
}

func buildAuthorizationURL(endpoint, clientID, redirectURI, state, verifier, resource, scope string) (string, error) {
	u, err := parseSecureOAuthURL(endpoint, true)
	if err != nil {
		return "", fmt.Errorf("MCP OAuth authorization endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", resource)
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func chooseTokenEndpointAuthMethod(supported []string) string {
	for _, preferred := range []string{"client_secret_basic", "client_secret_post", "none"} {
		if len(supported) == 0 || slices.Contains(supported, preferred) {
			return preferred
		}
	}
	return "none"
}

func protectedResourceMetadataURLs(endpoint *url.URL) []string {
	root := *endpoint
	root.RawQuery, root.Fragment = "", ""
	path := strings.TrimPrefix(root.EscapedPath(), "/")
	root.Path, root.RawPath = "/.well-known/oauth-protected-resource", ""
	urls := []string{}
	if path != "" {
		withPath := root
		withPath.Path += "/" + path
		urls = append(urls, withPath.String())
	}
	return append(urls, root.String())
}

func authorizationServerMetadataURLs(issuer *url.URL) []string {
	base := *issuer
	base.RawQuery, base.Fragment = "", ""
	issuerPath := strings.Trim(strings.TrimSpace(base.Path), "/")
	base.Path, base.RawPath = "", ""
	if issuerPath == "" {
		oauth := base
		oauth.Path = "/.well-known/oauth-authorization-server"
		oidc := base
		oidc.Path = "/.well-known/openid-configuration"
		return []string{oauth.String(), oidc.String()}
	}
	oauth := base
	oauth.Path = "/.well-known/oauth-authorization-server/" + issuerPath
	oidcInserted := base
	oidcInserted.Path = "/.well-known/openid-configuration/" + issuerPath
	oidcAppended := *issuer
	oidcAppended.Path = strings.TrimRight(oidcAppended.Path, "/") + "/.well-known/openid-configuration"
	return []string{oauth.String(), oidcInserted.String(), oidcAppended.String()}
}

func parseBearerChallenge(header string) (metadataURL, scope string, ok bool) {
	lower := strings.ToLower(header)
	for offset := 0; offset < len(header); {
		idx := strings.Index(lower[offset:], "bearer")
		if idx < 0 {
			return "", "", false
		}
		idx += offset
		beforeOK := idx == 0 || header[idx-1] == ',' || header[idx-1] == ' ' || header[idx-1] == '\t'
		after := idx + len("bearer")
		afterOK := after == len(header) || header[after] == ' ' || header[after] == '\t'
		if beforeOK && afterOK {
			params := parseAuthParams(header[after:])
			return params["resource_metadata"], params["scope"], true
		}
		offset = after
	}
	return "", "", false
}

func parseAuthParams(raw string) map[string]string {
	params := map[string]string{}
	for i := 0; i < len(raw); {
		i = skipAuthSeparators(raw, i)
		start, end := i, scanAuthToken(raw, i)
		if start == end {
			break
		}
		key := strings.ToLower(raw[start:end])
		i = skipAuthWhitespace(raw, end)
		if i >= len(raw) || raw[i] != '=' {
			break
		}
		value, next := parseAuthParamValue(raw, skipAuthWhitespace(raw, i+1))
		params[key], i = value, next
	}
	return params
}

func getOAuthJSON(ctx context.Context, client *http.Client, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return oauthHTTPError("GET "+rawURL, resp)
	}
	return decodeLimitedJSON(resp.Body, out)
}

func decodeLimitedJSON(r io.Reader, out any) error {
	decoder := json.NewDecoder(io.LimitReader(r, maxOAuthBody+1))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}

func oauthHTTPError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("%s: HTTP %d: %s", action, resp.StatusCode, secrets.RedactCredentials(strings.TrimSpace(string(body))))
}

func newOAuthHTTPClient(base *http.Client) *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	if base != nil {
		*client = *base
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
	}
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameHTTPOrigin(via[0].URL, req.URL) {
			return http.ErrUseLastResponse
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	return client
}

func parseSecureOAuthURL(raw string, allowLoopbackHTTP bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("invalid URL")
	}
	if strings.EqualFold(u.Scheme, "https") {
		return u, nil
	}
	host := strings.ToLower(u.Hostname())
	if allowLoopbackHTTP && strings.EqualFold(u.Scheme, "http") && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return u, nil
	}
	return nil, fmt.Errorf("URL must use HTTPS")
}

func sameCanonicalResource(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil || ua == nil || ub == nil {
		return false
	}
	// URL userinfo is explicit credential material, never an OAuth resource
	// identity. Do not let a credentialed URL match a credential-free state.
	if ua.User != nil || ub.User != nil {
		return false
	}
	ua.Fragment, ub.Fragment = "", ""
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host) && ua.EscapedPath() == ub.EscapedPath() && ua.RawQuery == ub.RawQuery
}

func isHTTPMCPTransport(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http", "streamable_http":
		return true
	default:
		return false
	}
}
