package mcpauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// defaultPublicClientID is used when no client id is configured and the
// authorization server does not advertise dynamic client registration. It
// matches the identifier Reasonix registers with servers that accept arbitrary
// public clients.
const defaultPublicClientID = "io.reasonix.mcp"

// refreshTimeout caps a non-interactive token refresh so a stalled token
// endpoint cannot block a request indefinitely.
const refreshTimeout = 30 * time.Second

// flowTimeout caps the interactive authorization flow (metadata discovery +
// registration + waiting for the redirect). It must exceed callbackWait.
const flowTimeout = callbackWait + 2*time.Minute

// Options configures a shared Client.
type Options struct {
	// StorePath is the credentials file. Empty uses DefaultStorePath().
	StorePath string
	// HTTPClient overrides the default outbound client (tests).
	HTTPClient *http.Client
	// Out receives user-facing status lines (e.g. "opening your browser").
	// nil discards them.
	Out io.Writer
	// OpenURL opens a URL in the user's browser. nil uses the platform default.
	OpenURL func(string) error
}

// Client is the MCP OAuth 2.0 client. It is safe for concurrent use and shared
// across all remote MCP servers in a process. Per-server OAuth behavior comes
// from the Config registered with SetConfig; servers with no config use the
// auto-discovery defaults.
type Client struct {
	store     *Store
	http      *http.Client
	out       io.Writer
	openURL   func(string) error
	keyLoader *keyLoader // memoizes private-key file reads for JWT assertions

	mu      sync.RWMutex
	configs map[string]Config // keyed by server name
}

// New constructs a Client. It does not create the credentials file; that happens
// lazily on the first successful authorization.
func New(opts Options) (*Client, error) {
	path := strings.TrimSpace(opts.StorePath)
	if path == "" {
		path = DefaultStorePath()
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	open := opts.OpenURL
	if open == nil {
		open = openBrowser
	}
	return &Client{
		store:     NewStore(path),
		http:      httpClient,
		out:       opts.Out,
		openURL:   open,
		keyLoader: newKeyLoader(),
		configs:   map[string]Config{},
	}, nil
}

// SetConfig registers the optional OAuth configuration for a server name. It is
// safe to call after New; later calls for the same name replace the config.
func (c *Client) SetConfig(serverName string, cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs[serverName] = cfg
}

// configFor returns the registered Config for serverName (zero value when none).
func (c *Client) configFor(serverName string) Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configs[serverName]
}

// CurrentToken returns a valid bearer-token Authorization-header value for
// serverURL if one is cached (refreshing non-interactively when possible), or
// "" when no token can be obtained without user interaction. It never blocks on
// the interactive flow.
func (c *Client) CurrentToken(serverURL string) string {
	origin := serverOrigin(serverURL)
	if origin == "" {
		return ""
	}
	cred, ok := c.store.Get(origin)
	if !ok || cred == nil {
		return ""
	}
	if !cred.Token.Expired() {
		return cred.Token.headerValue()
	}
	// Try a non-interactive refresh before giving up.
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	cfg := c.configFor("") // refresh is server-agnostic; config is looked up by name at Authorize
	tok, err := c.refresh(ctx, cred, cfg)
	if err != nil || tok == nil {
		return ""
	}
	updated := *cred
	updated.Token = *tok
	_ = c.store.Put(origin, &updated)
	return tok.headerValue()
}

// Authorize returns a bearer-token header value for serverURL, running the
// interactive authorization flow when no usable token is cached. The returned
// string is safe to send as an "Authorization" header value (e.g. "Bearer x").
func (c *Client) Authorize(ctx context.Context, serverName, serverURL string) (string, error) {
	origin := serverOrigin(serverURL)
	if origin == "" {
		return "", fmt.Errorf("server %q has no http(s) origin", serverName)
	}
	cfg := c.configFor(serverName)

	// Reuse a still-valid cached token.
	if cred, ok := c.store.Get(origin); ok && cred != nil && !cred.Token.Expired() {
		return cred.Token.headerValue(), nil
	}
	// Try a non-interactive refresh of a cached credential first.
	if cred, ok := c.store.Get(origin); ok && cred != nil && cred.Token.RefreshToken != "" {
		rctx, cancel := context.WithTimeout(ctx, refreshTimeout)
		tok, err := c.refresh(rctx, cred, cfg)
		cancel()
		if err == nil && tok != nil {
			updated := *cred
			updated.Token = *tok
			if err := c.store.Put(origin, &updated); err == nil {
				return tok.headerValue(), nil
			}
		}
		// Refresh failed: fall through to a fresh interactive flow.
	}

	// Run the interactive flow with its own bounded timeout so a missing browser
	// callback cannot wedge the request that triggered the 401.
	flowCtx, cancel := context.WithTimeout(ctx, flowTimeout)
	defer cancel()

	server, err := c.discover(flowCtx, origin, "", cfg)
	if err != nil {
		return "", fmt.Errorf("discover OAuth metadata for %q: %w", serverName, err)
	}

	// JWT bearer assertion grant (RFC 7523 §1): non-interactive service auth,
	// no browser. The grant is replayable (a fresh assertion per request) so
	// there is no refresh token to cache; we exchange and return directly.
	if cfg.JWTBearerGrant != nil {
		tok, err := c.exchangeAssertionGrant(flowCtx, server.AuthServer.TokenEndpoint, *cfg.JWTBearerGrant)
		if err != nil {
			return "", fmt.Errorf("jwt bearer grant for %q: %w", serverName, err)
		}
		cred := &StoredCredential{Token: *tok, Server: server}
		if err := c.store.Put(origin, cred); err != nil {
			return "", fmt.Errorf("persist OAuth token for %q: %w", serverName, err)
		}
		return tok.headerValue(), nil
	}

	cred, err := c.runAuthorizationFlow(flowCtx, serverName, serverURL, cfg, server)
	if err != nil {
		return "", err
	}
	if err := c.store.Put(origin, cred); err != nil {
		return "", fmt.Errorf("persist OAuth token for %q: %w", serverName, err)
	}
	c.notice("MCP %q authenticated successfully.", serverName)
	return cred.Token.headerValue(), nil
}

// discover resolves the protected-resource and authorization-server metadata for
// a server origin, reusing a cached ServerMetadata when available.
func (c *Client) discover(ctx context.Context, origin, wwwAuthenticate string, cfg Config) (*ServerMetadata, error) {
	trusted := cfg.TrustedOrigins
	prm, err := c.fetchProtectedResourceMetadata(ctx, origin, wwwAuthenticate, trusted)
	if err != nil {
		return nil, err
	}
	// Prefer the first advertised authorization server. (RFC 9728 allows
	// multiple; Reasonix uses the first.)
	asURL := origin
	if len(prm.AuthorizationServers) > 0 {
		asURL = prm.AuthorizationServers[0]
	}
	as, err := c.fetchAuthorizationServerMetadata(ctx, origin, asURL, trusted)
	if err != nil {
		return nil, err
	}
	return &ServerMetadata{
		ResourceURL:       origin,
		ProtectedResource: prm,
		AuthServer:        as,
	}, nil
}

// notice writes a user-facing status line unless output is disabled.
func (c *Client) notice(format string, args ...any) {
	if c.out == nil {
		return
	}
	fmt.Fprintf(c.out, format+"\n", args...)
}

// openBrowser opens urlStr in the user's default browser using the platform
// command. It is a best-effort helper; failures fall back to printing the URL.
func openBrowser(urlStr string) error {
	cmd, err := browserCommand(urlStr)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func browserCommand(urlStr string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", urlStr), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", urlStr), nil
	default:
		return exec.Command("xdg-open", urlStr), nil
	}
}

// DefaultStorePath returns the credentials file path under the user's reasonix
// config directory. It mirrors the layout used by other per-user state.
func DefaultStorePath() string {
	dir := os.Getenv("REASONIX_HOME")
	if dir == "" {
		if c, err := os.UserConfigDir(); err == nil && c != "" {
			dir = c
		} else if h, err := os.UserHomeDir(); err == nil && h != "" {
			dir = h
		} else {
			dir = "."
		}
	}
	return strings.TrimRight(dir, string(os.PathSeparator)) + string(os.PathSeparator) + "mcp-oauth-credentials.json"
}
