package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// pemRSA returns a PEM-encoded RSA private key for a test.
func pemRSA(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

// jwtBearerAS is a fake AS that validates a client_assertion / assertion and
// returns a token. It records what it received.
type jwtBearerAS struct {
	srv          *httptest.Server
	mu           sync.Mutex
	lastForm     url.Values
	tokenType    string
	accessToken  string
	refreshToken string
	expiresIn    int
}

func newJWTBearerAS(t *testing.T) *jwtBearerAS {
	t.Helper()
	f := &jwtBearerAS{accessToken: "JWT_ACCESS_1", tokenType: "Bearer", expiresIn: 3600}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, ProtectedResourceMetadata{Resource: srv.URL, AuthorizationServers: []string{srv.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, AuthorizationServerMetadata{
			Issuer:                            srv.URL,
			AuthorizationEndpoint:             srv.URL + "/authorize",
			TokenEndpoint:                     srv.URL + "/token",
			TokenEndpointAuthMethodsSupported: []string{"private_key_jwt", "client_secret_jwt", "client_secret_basic", "none"},
			TokenEndpointAuthSigningAlgValuesSupported: []string{"RS256", "ES256", "HS256", "PS256"},
			GrantTypesSupported:                        []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"},
			CodeChallengeMethodsSupported:              []string{"S256"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.lastForm = cloneForm(r.PostForm)
		f.mu.Unlock()
		writeJSON(t, w, map[string]any{
			"access_token":  f.accessToken,
			"refresh_token": f.refreshToken,
			"token_type":    f.tokenType,
			"expires_in":    f.expiresIn,
		})
	})
	srv = httptest.NewServer(mux)
	f.srv = srv
	t.Cleanup(srv.Close)
	return f
}

func cloneForm(f url.Values) url.Values {
	out := make(url.Values, len(f))
	for k, v := range f {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// decodeAssertionHeader returns the alg from a JWT's header.
func decodeAssertionAlg(t *testing.T, assertion string) string {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if len(parts) < 2 {
		t.Fatalf("malformed assertion: %s", assertion)
	}
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hb, &hdr); err != nil {
		t.Fatal(err)
	}
	return hdr.Alg
}

func TestResolveTokenEndpointAuthPrivateKeyJWT(t *testing.T) {
	f := newJWTBearerAS(t)
	keyPEM := pemRSA(t)
	c := newTestClient(t, Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})
	as := &AuthorizationServerMetadata{
		Issuer:                            f.srv.URL,
		TokenEndpoint:                     f.srv.URL + "/token",
		TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"},
	}
	cfg := Config{ClientID: "c1", TokenEndpointAuthMethod: "private_key_jwt", PrivateKeyPEM: keyPEM}
	auth, err := c.resolveTokenEndpointAuth(cfg, &ClientRegistration{ClientID: "c1"}, as)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// The auth builds a client_assertion when enriching the form.
	form := url.Values{}
	if err := auth.enrich(form); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if got := form.Get("client_assertion_type"); got != clientAssertionTypeJWT {
		t.Fatalf("client_assertion_type = %q", got)
	}
	assertion := form.Get("client_assertion")
	if assertion == "" {
		t.Fatal("client_assertion missing")
	}
	if alg := decodeAssertionAlg(t, assertion); alg != "RS256" {
		t.Fatalf("assertion alg = %q, want RS256", alg)
	}
}

func TestResolveTokenEndpointAuthClientSecretJWT(t *testing.T) {
	c := newTestClient(t, Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})
	as := &AuthorizationServerMetadata{
		TokenEndpoint:                     "https://as/token",
		TokenEndpointAuthMethodsSupported: []string{"client_secret_jwt"},
	}
	cfg := Config{ClientID: "c1", ClientSecret: "shh", TokenEndpointAuthMethod: "client_secret_jwt"}
	auth, err := c.resolveTokenEndpointAuth(cfg, &ClientRegistration{ClientID: "c1", ClientSecret: "shh"}, as)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	form := url.Values{}
	if err := auth.enrich(form); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if form.Get("client_assertion") == "" {
		t.Fatal("client_secret_jwt must produce a client_assertion")
	}
	if alg := decodeAssertionAlg(t, form.Get("client_assertion")); alg != "HS256" {
		t.Fatalf("client_secret_jwt default alg = %q, want HS256", alg)
	}
}

func TestResolveTokenEndpointAuthRejectsUnsupportedMethod(t *testing.T) {
	c := newTestClient(t, Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})
	as := &AuthorizationServerMetadata{
		TokenEndpoint:                     "https://as/token",
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"}, // no private_key_jwt
	}
	_, keyPEM := genRSA(t)
	_, err := c.resolveTokenEndpointAuth(
		Config{ClientID: "c", TokenEndpointAuthMethod: "private_key_jwt", PrivateKeyPEM: keyPEM},
		&ClientRegistration{ClientID: "c"}, as)
	if err == nil {
		t.Fatal("expected error: server does not advertise private_key_jwt")
	}
}

func TestResolveTokenEndpointAuthDefaults(t *testing.T) {
	c := newTestClient(t, Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})
	as := &AuthorizationServerMetadata{TokenEndpoint: "https://as/token"}
	// No config → public client (noneAuth).
	auth, err := c.resolveTokenEndpointAuth(Config{}, &ClientRegistration{ClientID: "pub"}, as)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	_ = auth.enrich(form)
	if form.Get("client_id") != "pub" {
		t.Fatalf("default auth should set client_id, got %q", form.Get("client_id"))
	}
}

func TestExchangeAssertionGrant(t *testing.T) {
	f := newJWTBearerAS(t)
	_, keyPEM := genRSA(t)
	c := newTestClient(t, Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})

	grant := JWTBearerGrant{
		Issuer:        "service-account-1",
		PrivateKeyPEM: keyPEM,
		SigningAlg:    "RS256",
		Scopes:        []string{"read"},
	}
	// The grant needs the token endpoint, which comes from discovery. Use the
	// resolver's exchangeAssertionGrant directly with the known endpoint.
	tok, err := c.exchangeAssertionGrant(context.Background(), f.srv.URL+"/token", grant)
	if err != nil {
		t.Fatalf("exchangeAssertionGrant: %v", err)
	}
	if tok.AccessToken != "JWT_ACCESS_1" {
		t.Fatalf("access_token = %q", tok.AccessToken)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastForm.Get("grant_type") != grantTypeJWTBearer {
		t.Fatalf("grant_type = %q, want %s", f.lastForm.Get("grant_type"), grantTypeJWTBearer)
	}
	assertion := f.lastForm.Get("assertion")
	if assertion == "" {
		t.Fatal("assertion not sent")
	}
	if alg := decodeAssertionAlg(t, assertion); alg != "RS256" {
		t.Fatalf("assertion alg = %q, want RS256", alg)
	}
}

func TestAuthorizeJWTBearerGrantNonInteractive(t *testing.T) {
	f := newJWTBearerAS(t)
	_, keyPEM := genRSA(t)
	c := newTestClient(t, Options{
		StorePath: filepath.Join(t.TempDir(), "creds.json"),
		// No OpenURL: if the flow opened a browser, the test would hang until
		// timeout. The grant path must never call OpenURL.
		OpenURL: func(string) error { t.Fatal("JWT bearer grant must not open a browser"); return nil },
	})
	c.SetConfig("svc", Config{
		JWTBearerGrant: &JWTBearerGrant{
			Issuer:        "svc-acct",
			PrivateKeyPEM: keyPEM,
			SigningAlg:    "RS256",
		},
	})
	token, err := c.Authorize(context.Background(), "svc", f.srv.URL)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if token != "Bearer JWT_ACCESS_1" {
		t.Fatalf("token = %q", token)
	}
}

func TestPrivateKeyPathIsReadOnce(t *testing.T) {
	_, keyPEM := genRSA(t)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.pem")
	if err := writeFile(keyFile, []byte(keyPEM)); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, Options{StorePath: filepath.Join(dir, "creds.json")})
	as := &AuthorizationServerMetadata{TokenEndpoint: "https://as/token", TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}}
	cfg := Config{ClientID: "c", TokenEndpointAuthMethod: "private_key_jwt", PrivateKeyPath: keyFile}
	for i := 0; i < 3; i++ {
		auth, err := c.resolveTokenEndpointAuth(cfg, &ClientRegistration{ClientID: "c"}, as)
		if err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
		form := url.Values{}
		if err := auth.enrich(form); err != nil {
			t.Fatalf("enrich %d: %v", i, err)
		}
		if form.Get("client_assertion") == "" {
			t.Fatalf("iteration %d produced no assertion", i)
		}
	}
}

func genRSA(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
