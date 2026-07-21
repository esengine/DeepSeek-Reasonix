# MCP OAuth 2.0 Authorization

Reasonix can connect to remote MCP servers (Streamable HTTP, `type = "http"`)
that require OAuth 2.0 authorization — for example hosted servers behind a
social login, or managed registries such as Composio and Amplitude. Authorization
runs automatically the first time a server rejects a request with `401`.

## How it works

1. **401 triggers discovery.** When a remote server returns `401 Unauthorized`,
   Reasonix fetches the server's
   [protected-resource metadata](https://datatracker.ietf.org/doc/html/rfc9728)
   at `/.well-known/oauth-protected-resource`, then the linked
   [authorization-server metadata](https://datatracker.ietf.org/doc/html/rfc8414).
2. **Client registration.** If the authorization server advertises a
   [dynamic registration endpoint](https://datatracker.ietf.org/doc/html/rfc7591)
   (RFC 7591), Reasonix registers a public PKCE client. Otherwise it uses an
   explicit `client_id` from config or a default public client id.
3. **Sign in.** Reasonix opens your default browser at the authorization
   endpoint and listens on a loopback redirect (`http://localhost:<port>/callback`)
   for the code. The flow uses PKCE (S256) and a `state` CSRF token.
4. **Token exchange & caching.** The code is exchanged for an access token
   (and refresh token when issued). The token is stored at
   `~/.reasonix/mcp-oauth-credentials.json` (mode `0600`), keyed by server
   origin, so later sessions reuse it without another sign-in.
5. **Refresh.** When the cached token expires, Reasonix refreshes it
   non-interactively. If the refresh token is no longer valid, the browser
   flow runs again on the next `401`.

No new dependencies were added; the implementation uses only the Go standard
library.

## Minimal configuration

For servers that support auto-discovery, you need nothing beyond the server URL:

```toml
[[plugins]]
name = "composio"
type = "http"
url  = "https://mcp.composio.dev/app/..."
```

The first request gets a `401`; Reasonix opens your browser, you sign in, and
the server's tools come online.

## Optional overrides

Pin behavior with an `[plugins.oauth]` block. Every field is optional:

```toml
[[plugins]]
name = "amplitude"
type = "http"
url  = "https://mcp.amplitude.com/..."

[plugins.oauth]
client_id = "my-public-client"      # default: dynamic client registration
client_secret = ""                  # only for confidential clients
scopes = ["read", "write"]          # default: server-advertised scopes
redirect_port = 8080                # default: a free loopback port
skip_browser = false                # true prints the URL instead (headless/CI)
skip_dynamic_registration = false   # true disables RFC 7591 DCR
trusted_origins = []                # allow metadata/AS origins beyond the server
```

The same block works in a project-root `.mcp.json` (Claude-compatible):

```json
{
  "mcpServers": {
    "amplitude": {
      "type": "http",
      "url": "https://mcp.amplitude.com/...",
      "oauth": { "scopes": ["read"] }
    }
  }
}
```

## Headless / CI

Set `skip_browser = true`. Reasonix prints the authorization URL and waits (up
to five minutes) for you to open it and complete the sign-in from any browser
that can reach the loopback callback. For fully automated environments, prefer a
server that accepts a static bearer token configured in `headers` instead.

## Security

- **Origin validation.** Protected-resource and authorization-server metadata
  must come from the MCP server's own origin (or an explicitly trusted origin
  via `trusted_origins`). The issuer in AS metadata must match the URL it was
  fetched from. This prevents a malicious server from redirecting authorization
  to attacker-controlled endpoints.
- **PKCE + state.** Every authorization uses S256 PKCE and a random `state`
  token validated on the callback.
- **Token storage.** Credentials are written to a `0600` file, atomically, and
  never appear in logs. The `clear-auth` command also strips any configured
  OAuth `client_secret` before sharing config.
- **Redirect isolation.** The configured `Authorization` header is never sent
  across an origin redirect.
- **Key handling.** Inline private keys (`private_key_pem`) are stripped by
  `clear-auth` like any other secret; key file paths (`private_key_path`) are
  preserved since they are pointers, not secrets.

## OAuth 2.1 compliance

The client follows the [OAuth 2.1](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/)
best-current-practice profile, which is a constrained subset of OAuth 2.0:

- **PKCE is always used** (S256 only; the `plain` method is never sent), even
  for confidential clients.
- **Only the authorization-code grant** is used for interactive flows. The
  implicit and resource-owner-password grants are never sent.
- **Exact `redirect_uri` matching** — the same loopback URI is sent to both the
  authorize and token endpoints.
- **`state` CSRF protection** on every authorization.
- JWT-based token-endpoint authentication (`private_key_jwt`,
  `client_secret_jwt`) is supported per OAuth 2.1's recommendation for
  confidential clients.

## OAuth/JWT (RFC 7523)

The client supports the JWT Profile for OAuth 2.0 (RFC 7523) in two ways,
implemented with the Go standard library only (no JOSE dependency):

### JWT access tokens

JWT-format access tokens returned by a server are consumed transparently: any
token string the server returns is sent as `Bearer <token>`. No configuration
is needed.

### JWT client authentication (`private_key_jwt` / `client_secret_jwt`)

Instead of sending a `client_secret`, the client signs a short-lived JWT
assertion that authenticates it at the token endpoint (RFC 7523 §2). This is how
services prove identity with an asymmetric key and is recommended for
confidential clients.

```toml
[[plugins]]
name = "svc"
type = "http"
url  = "https://mcp.example.com/mcp"

[plugins.oauth]
client_id = "my-client"
token_endpoint_auth_method = "private_key_jwt"
private_key_path = "/etc/reasonix/svc.pem"   # RSA/EC/Ed25519 PEM
client_assertion_signing_alg = "RS256"        # optional; defaults to key type
```

Supported signing algorithms: `RS256/384/512`, `PS256/384/512`, `ES256/384/512`,
`HS256/384/512` (for `client_secret_jwt`), and `EdDSA`. The auth method must be
advertised in the server's `token_endpoint_auth_methods_supported` or the
authorization fails loudly.

### JWT bearer assertion grant (service-to-service)

For non-interactive machine authorization (no browser), configure a JWT bearer
assertion grant (RFC 7523 §1). The client signs a fresh assertion for each token
request:

```toml
[[plugins]]
name = "robot"
type = "http"
url  = "https://mcp.example.com/mcp"

[plugins.oauth.jwt_bearer_grant]
issuer = "service-account-1"
private_key_path = "/etc/reasonix/robot.pem"
signing_alg = "RS256"
scopes = ["tools:call"]
```

When a `jwt_bearer_grant` is set, `Authorize` never opens a browser.

## Troubleshooting

- **`http 401: server requires OAuth authorization`** after signing in — the
  token was rejected. Check that `client_id`/`scopes` match what the server
  expects, or remove the overrides to let auto-discovery choose them.
- **`discover OAuth metadata ... 404`** — the server does not publish
  `/.well-known/oauth-protected-resource`. Confirm the server supports MCP
  OAuth; if it only accepts a static token, use `headers` instead.
- **`authorization server requires PAR`** — the server mandates Pushed
  Authorization Requests (RFC 9126), which is not yet supported.
