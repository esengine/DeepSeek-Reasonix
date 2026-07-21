package mcpauth

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// RFC 7523 §2 client assertion types and grant types.
const (
	clientAssertionTypeJWT = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	grantTypeJWTBearer     = "urn:ietf:params:oauth:grant-type:jwt-bearer"
)

// jwtClientAuth authenticates a client at the token endpoint by signing a JWT
// client assertion (RFC 7523 §2). It implements private_key_jwt (asymmetric
// key) and client_secret_jwt (HMAC with the client secret). The assertion is
// rebuilt on every token request because exp/jti must be fresh; a signing
// failure surfaces as the request error rather than an unauthenticated call.
type jwtClientAuth struct {
	clientID      string
	tokenEndpoint string
	signingKey    crypto.PrivateKey
	alg           string
	hmacSecret    []byte // non-nil when this is client_secret_jwt
}

// enrich signs a fresh client assertion and adds it to the form. The assertion
// carries the client identity, so client_id is intentionally omitted (RFC 7523
// §2.2: the assertion IS the authentication).
func (a jwtClientAuth) enrich(form url.Values) error {
	assertion, err := a.buildAssertion()
	if err != nil {
		return err
	}
	form.Set("client_assertion_type", clientAssertionTypeJWT)
	form.Set("client_assertion", assertion)
	return nil
}

func (a jwtClientAuth) apply(*http.Request) {}

// buildAssertion signs a fresh client-assertion JWT for this token request.
func (a jwtClientAuth) buildAssertion() (string, error) {
	if a.signingKey == nil && len(a.hmacSecret) == 0 {
		return "", errors.New("no signing key for JWT client assertion")
	}
	key := a.signingKey
	if a.signingKey == nil {
		key = a.hmacSecret
	}
	alg := a.alg
	if alg == "" {
		var err error
		if alg, err = defaultAlgFor(key); err != nil {
			return "", err
		}
	}
	if !algSupportsKey(alg, key) {
		return "", fmt.Errorf("configured signing algorithm %q does not match the key type", alg)
	}
	claims := assertionClaims(a.clientID, a.tokenEndpoint)
	return signJWT(claims, key, alg)
}

// resolveTokenEndpointAuth picks the token-endpoint authentication strategy from
// the explicit config, then the client registration, then sensible defaults
// based on available credentials and server metadata.
func (c *Client) resolveTokenEndpointAuth(cfg Config, reg *ClientRegistration, as *AuthorizationServerMetadata) (clientAuth, error) {
	clientID := ""
	if reg != nil {
		clientID = reg.ClientID
	}
	secret := strings.TrimSpace(cfg.ClientSecret)
	if secret == "" && reg != nil {
		secret = reg.ClientSecret
	}
	method := strings.ToLower(strings.TrimSpace(cfg.TokenEndpointAuthMethod))

	// Load the private key once for private_key_jwt (cached per client).
	if method == "private_key_jwt" || (method == "" && cfg.hasPrivateKey()) {
		key, alg, err := cfg.loadSigningKey(c.keyLoader)
		if err != nil {
			if method == "private_key_jwt" {
				return nil, fmt.Errorf("private_key_jwt: %w", err)
			}
			// key unavailable for auto mode: fall through to other methods
		} else {
			if !asMethodSupported(as, "private_key_jwt") {
				return nil, fmt.Errorf("client configured private_key_jwt but the authorization server does not advertise it")
			}
			return jwtClientAuth{clientID: clientID, tokenEndpoint: as.TokenEndpoint, signingKey: key, alg: alg}, nil
		}
	}

	switch method {
	case "client_secret_jwt":
		if secret == "" {
			return nil, errors.New("client_secret_jwt requires a client_secret")
		}
		if !asMethodSupported(as, "client_secret_jwt") {
			return nil, fmt.Errorf("client configured client_secret_jwt but the authorization server does not advertise it")
		}
		alg := cfg.ClientAssertionSigningAlg
		if alg == "" {
			alg = "HS256"
		}
		return jwtClientAuth{clientID: clientID, tokenEndpoint: as.TokenEndpoint, hmacSecret: []byte(secret), alg: alg}, nil
	case "client_secret_basic":
		if secret == "" {
			return nil, errors.New("client_secret_basic requires a client_secret")
		}
		return basicAuth{clientID: clientID, clientSecret: secret}, nil
	case "client_secret_post":
		if secret == "" {
			return nil, errors.New("client_secret_post requires a client_secret")
		}
		return secretPostAuth{clientID: clientID, clientSecret: secret}, nil
	case "none", "":
		return noneAuth{clientID: clientID}, nil
	default:
		return nil, fmt.Errorf("unsupported token_endpoint_auth_method %q", method)
	}
}

// asMethodSupported reports whether the authorization server advertises a
// token_endpoint_auth_method (or omits the list, treated as permissive).
func asMethodSupported(as *AuthorizationServerMetadata, method string) bool {
	if as == nil || len(as.TokenEndpointAuthMethodsSupported) == 0 {
		return true
	}
	for _, m := range as.TokenEndpointAuthMethodsSupported {
		if strings.EqualFold(strings.TrimSpace(m), method) {
			return true
		}
	}
	return false
}

// secretPostAuth authenticates with client_secret in the form body
// (client_secret_post).
type secretPostAuth struct{ clientID, clientSecret string }

func (a secretPostAuth) enrich(form url.Values) error {
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	return nil
}
func (a secretPostAuth) apply(*http.Request) {}

// exchangeAssertionGrant exchanges an RFC 7523 §1 JWT bearer assertion for a
// token, with no user interaction. Used when a server is configured with a
// JWTBearerGrant (service-to-service).
func (c *Client) exchangeAssertionGrant(ctx context.Context, tokenEndpoint string, grant JWTBearerGrant) (*Token, error) {
	key, alg, err := grant.loadSigningKey(c.keyLoader)
	if err != nil {
		return nil, fmt.Errorf("load assertion signing key: %w", err)
	}
	issuer := firstNonEmpty(grant.Issuer, grant.Subject)
	subject := firstNonEmpty(grant.Subject, grant.Issuer)
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("jwt bearer grant requires an issuer")
	}
	claims := assertionClaims(subject, tokenEndpoint)
	claims["iss"] = issuer
	if alg == "" {
		if alg, err = defaultAlgFor(key); err != nil {
			return nil, err
		}
	}
	assertion, err := signJWT(claims, key, alg)
	if err != nil {
		return nil, fmt.Errorf("sign jwt bearer assertion: %w", err)
	}
	form := url.Values{
		"grant_type": {grantTypeJWTBearer},
		"assertion":  {assertion},
	}
	if len(grant.Scopes) > 0 {
		form.Set("scope", strings.Join(grant.Scopes, " "))
	}
	respBody, err := c.postForm(ctx, tokenEndpoint, form, noneAuth{clientID: issuer})
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(respBody)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// hasPrivateKey reports whether the config can supply a JWT signing key.
func (cfg Config) hasPrivateKey() bool {
	return strings.TrimSpace(cfg.PrivateKeyPEM) != "" || strings.TrimSpace(cfg.PrivateKeyPath) != ""
}

// loadSigningKey resolves the configured signing key and (optional) algorithm.
// The PEM is loaded lazily through the client's cached loader so a key file is
// read at most once per process.
func (cfg Config) loadSigningKey(loader *keyLoader) (crypto.PrivateKey, string, error) {
	pemData := strings.TrimSpace(cfg.PrivateKeyPEM)
	if pemData == "" && strings.TrimSpace(cfg.PrivateKeyPath) != "" {
		data, err := loader.load(strings.TrimSpace(cfg.PrivateKeyPath))
		if err != nil {
			return nil, "", err
		}
		pemData = data
	}
	if pemData == "" {
		return nil, "", errors.New("no private key configured")
	}
	key, err := parsePrivateKey([]byte(pemData))
	if err != nil {
		return nil, "", err
	}
	return key, cfg.ClientAssertionSigningAlg, nil
}

func (g JWTBearerGrant) loadSigningKey(loader *keyLoader) (crypto.PrivateKey, string, error) {
	pemData := strings.TrimSpace(g.PrivateKeyPEM)
	if pemData == "" && strings.TrimSpace(g.PrivateKeyPath) != "" {
		data, err := loader.load(strings.TrimSpace(g.PrivateKeyPath))
		if err != nil {
			return nil, "", err
		}
		pemData = data
	}
	if pemData == "" {
		return nil, "", errors.New("no private key configured for jwt bearer grant")
	}
	key, err := parsePrivateKey([]byte(pemData))
	if err != nil {
		return nil, "", err
	}
	return key, g.SigningAlg, nil
}

// keyLoader reads key files and memoizes the result so a key file is parsed at
// most once per process (keys are large and parsing is non-trivial).
type keyLoader struct {
	mu    sync.Mutex
	cache map[string]string
}

func newKeyLoader() *keyLoader { return &keyLoader{cache: map[string]string{}} }

func (l *keyLoader) load(path string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := l.cache[path]; ok {
		return v, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read private key %s: %w", path, err)
	}
	s := string(data)
	l.cache[path] = s
	return s, nil
}
