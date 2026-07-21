package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// callbackWait is how long the flow waits for the user to complete the browser
// login after the authorization URL is opened.
const callbackWait = 5 * time.Minute

// runAuthorizationFlow performs the interactive authorization-code + PKCE flow
// for one server and returns the resulting stored credential. server is the
// already-discovered metadata; it may be nil only when the caller could not
// discover any (in which case the flow cannot run).
func (c *Client) runAuthorizationFlow(ctx context.Context, serverName, serverURL string, cfg Config, server *ServerMetadata) (*StoredCredential, error) {
	if server == nil || server.AuthServer == nil {
		return nil, fmt.Errorf("no authorization-server metadata discovered for %q", serverURL)
	}
	as := server.AuthServer
	origin := serverOrigin(serverURL)

	// 1. Bind the loopback redirect listener up front so the redirect_uri is
	//    stable for both dynamic registration and the authorization request.
	listener, redirectURI, err := c.bindCallback(cfg.RedirectPort)
	if err != nil {
		return nil, err
	}
	resultCh := make(chan callbackResult, 1)
	srv := &http.Server{Handler: c.callbackHandler(resultCh)}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	// 2. Resolve the client identity: explicit config, then dynamic registration
	//    (when advertised and allowed), then a default public id.
	reg := c.resolveClientID(cfg)
	if reg.ClientID == "" && as.RegistrationEndpoint != "" && !cfg.SkipDynamicRegistration {
		registered, dcrErr := c.registerClient(ctx, as.RegistrationEndpoint, redirectURI)
		if dcrErr != nil {
			// DCR is optional; a server that advertises an endpoint but rejects
			// registration may still accept a default public client id. Fall
			// through and let the flow try.
			c.notice("MCP %q: dynamic client registration failed (%v); continuing with a public client", serverName, dcrErr)
		} else {
			reg = registered
		}
	}
	if reg.ClientID == "" {
		reg = &ClientRegistration{ClientID: defaultPublicClientID}
	}

	// 3. PKCE.
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, err
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	// 4. Build and present the authorization URL.
	authURL := buildAuthorizationURL(as.AuthorizationEndpoint, reg.ClientID, redirectURI, verifier, state, effectiveScopes(cfg, as), origin)
	if cfg.SkipBrowser {
		c.notice("MCP %q requires sign-in. Open this URL in your browser:\n  %s", serverName, authURL)
	} else {
		c.notice("MCP %q requires sign-in; opening your browser...", serverName)
		if err := c.openURL(authURL); err != nil {
			c.notice("could not open browser automatically (%v). Open this URL:\n  %s", err, authURL)
		}
	}

	// 5. Wait for the redirect.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(callbackWait):
		return nil, fmt.Errorf("timed out waiting for authorization callback for %q", serverName)
	case res := <-resultCh:
		if res.errDescription != "" {
			return nil, fmt.Errorf("authorization server rejected the request for %q: %s", serverName, res.errDescription)
		}
		if res.code == "" {
			return nil, fmt.Errorf("authorization callback for %q returned no code", serverName)
		}
		if res.state != state {
			return nil, fmt.Errorf("authorization callback state mismatch for %q (possible CSRF)", serverName)
		}

		// 6. Exchange the code for a token.
		token, err := c.exchangeCode(ctx, as.TokenEndpoint, res.code, redirectURI, verifier, cfg, reg, as)
		if err != nil {
			return nil, fmt.Errorf("exchange authorization code for %q: %w", serverName, err)
		}
		cred := &StoredCredential{Token: *token, Server: server, Client: reg}
		if reg.hasSecret() {
			cred.Client.ClientSecret = reg.ClientSecret
		}
		return cred, nil
	}
}

// callbackResult carries the outcome of the loopback redirect.
type callbackResult struct {
	code, state, errDescription string
}

// bindCallback listens on a loopback port and returns the redirect_uri to use.
func (c *Client) bindCallback(port int) (net.Listener, string, error) {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Fall back to any free port.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, "", fmt.Errorf("bind loopback callback: %w", err)
		}
	}
	host := ln.Addr().(*net.TCPAddr)
	redirectURI := "http://localhost:" + strconv.Itoa(host.Port) + callbackPath
	return ln, redirectURI, nil
}

const callbackPath = "/callback"

// callbackHandler returns the HTTP handler that captures the authorization
// response and signals resultCh exactly once.
func (c *Client) callbackHandler(resultCh chan<- callbackResult) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := callbackResult{
			code:           q.Get("code"),
			state:          q.Get("state"),
			errDescription: describeError(q),
		}
		once.Do(func() {
			select {
			case resultCh <- res:
			default:
			}
		})
		// The browser shows this to the user after the redirect.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.errDescription != "" || res.code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, callbackFailureHTML(res.errDescription))
			return
		}
		_, _ = io.WriteString(w, callbackSuccessHTML)
	}
}

func describeError(q url.Values) string {
	if e := strings.TrimSpace(q.Get("error")); e != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		if desc == "" {
			return e
		}
		return e + ": " + desc
	}
	return ""
}

// exchangeCode trades an authorization code for tokens at the token endpoint.
func (c *Client) exchangeCode(ctx context.Context, tokenEndpoint, code, redirectURI, verifier string, cfg Config, reg *ClientRegistration, as *AuthorizationServerMetadata) (*Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	auth, err := c.resolveTokenEndpointAuth(cfg, reg, as)
	if err != nil {
		return nil, fmt.Errorf("token endpoint auth: %w", err)
	}
	respBody, err := c.postForm(ctx, tokenEndpoint, form, auth)
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(respBody)
}

// refresh obtains a fresh access token using a stored refresh token. It performs
// no user interaction. A non-nil error means the refresh token is no longer
// usable and a full flow is required.
func (c *Client) refresh(ctx context.Context, cred *StoredCredential, cfg Config) (*Token, error) {
	if cred == nil || cred.Token.RefreshToken == "" || cred.Server == nil || cred.Server.AuthServer == nil {
		return nil, errNoRefreshToken
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.Token.RefreshToken},
	}
	if scopes := effectiveScopes(cfg, cred.Server.AuthServer); len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	auth, err := c.resolveTokenEndpointAuth(cfg, cred.Client, cred.Server.AuthServer)
	if err != nil {
		return nil, fmt.Errorf("token endpoint auth: %w", err)
	}
	respBody, err := c.postForm(ctx, cred.Server.AuthServer.TokenEndpoint, form, auth)
	if err != nil {
		return nil, err
	}
	tok, err := parseTokenResponse(respBody)
	if err != nil {
		return nil, err
	}
	// RFC 6749 §6: a refreshed token may omit refresh_token, in which case the
	// original remains valid.
	if tok.RefreshToken == "" {
		tok.RefreshToken = cred.Token.RefreshToken
	}
	return tok, nil
}

var errNoRefreshToken = errors.New("no refresh token available")

// parseTokenResponse decodes a token-endpoint JSON response into a Token.
func parseTokenResponse(body []byte) (*Token, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	tok := &Token{}
	if v, ok := raw["access_token"]; ok {
		_ = json.Unmarshal(v, &tok.AccessToken)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	if v, ok := raw["refresh_token"]; ok {
		_ = json.Unmarshal(v, &tok.RefreshToken)
	}
	if v, ok := raw["token_type"]; ok {
		_ = json.Unmarshal(v, &tok.TokenType)
	}
	if v, ok := raw["scope"]; ok {
		_ = json.Unmarshal(v, &tok.Scope)
	}
	if v, ok := raw["expires_in"]; ok {
		var secs int64
		if err := json.Unmarshal(v, &secs); err == nil && secs > 0 {
			tok.Expiry = time.Now().Add(time.Duration(secs) * time.Second)
		}
	}
	return tok, nil
}

// resolveClientID returns a client registration from explicit config, if set.
func (c *Client) resolveClientID(cfg Config) *ClientRegistration {
	id := strings.TrimSpace(cfg.ClientID)
	if id == "" {
		return &ClientRegistration{}
	}
	return &ClientRegistration{ClientID: id, ClientSecret: strings.TrimSpace(cfg.ClientSecret)}
}

// effectiveScopes returns the scopes to request: explicit config wins, then the
// server-advertised scopes, then none.
func effectiveScopes(cfg Config, as *AuthorizationServerMetadata) []string {
	if len(cfg.Scopes) > 0 {
		return cfg.Scopes
	}
	if as != nil && len(as.ScopesSupported) > 0 {
		return as.ScopesSupported
	}
	return nil
}

// buildAuthorizationURL constructs the authorization endpoint URL with PKCE and
// the MCP resource indicator.
func buildAuthorizationURL(authorizationEndpoint, clientID, redirectURI, verifier, state string, scopes []string, resource string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {codeChallengeS256(verifier)},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if resource != "" {
		q.Set("resource", resource)
	}
	sep := "?"
	if strings.Contains(authorizationEndpoint, "?") {
		sep = "&"
	}
	return authorizationEndpoint + sep + q.Encode()
}

const callbackSuccessHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Authorized</title></head>
<body style="font-family:system-ui,sans-serif;max-width:34rem;margin:4rem auto;padding:0 1rem">
<h2>Authorization complete</h2>
<p>You can close this tab and return to Reasonix.</p></body></html>`

func callbackFailureHTML(reason string) string {
	if reason == "" {
		reason = "no authorization code was returned"
	}
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>Authorization failed</title></head>
<body style="font-family:system-ui,sans-serif;max-width:34rem;margin:4rem auto;padding:0 1rem">
<h2>Authorization failed</h2><p>%s</p>
<p>Return to Reasonix and retry.</p></body></html>`, htmlEscape(reason))
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
