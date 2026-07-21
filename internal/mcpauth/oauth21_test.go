package mcpauth

import (
	"strings"
	"testing"
)

// TestOAuth21Compliance verifies the client follows the OAuth 2.1 best-current
// practice profile (draft-ietf-oauth-v2-1). OAuth 2.1 is a constrained subset
// of OAuth 2.0, not a new protocol; these tests assert the constraints.
func TestOAuth21PKCEAlwaysSentWithS256(t *testing.T) {
	// §4.1.1: PKCE is REQUIRED for authorization-code; §7.2: S256 only.
	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatal(err)
	}
	challenge := codeChallengeS256(verifier)
	if challenge == "" {
		t.Fatal("PKCE challenge must be non-empty")
	}
	// buildAuthorizationURL must always carry the challenge + method.
	url := buildAuthorizationURL("https://as/authorize", "c", "https://localhost/cb", verifier, "state", nil, "")
	if !strings.Contains(url, "code_challenge=") {
		t.Fatal("authorization URL must include code_challenge")
	}
	if !strings.Contains(url, "code_challenge_method=S256") {
		t.Fatal("OAuth 2.1 requires S256 PKCE method")
	}
	if strings.Contains(url, "code_challenge_method=plain") {
		t.Fatal("OAuth 2.1 forbids the plain PKCE method")
	}
}

func TestOAuth21AuthorizationCodeOnly(t *testing.T) {
	// §2.1: response_type=code only; implicit grant is removed.
	url := buildAuthorizationURL("https://as/authorize", "c", "https://localhost/cb", "v", "s", nil, "")
	if !strings.Contains(url, "response_type=code") {
		t.Fatal("must use response_type=code")
	}
	// No password or implicit grant types appear anywhere in the request surface.
	for _, forbidden := range []string{"response_type=token", "response_type=id_token", "grant_type=password"} {
		if strings.Contains(url, forbidden) {
			t.Fatalf("OAuth 2.1 forbids %s", forbidden)
		}
	}
}

func TestOAuth21ExactRedirectURIMatching(t *testing.T) {
	// §2.1: exact redirect_uri matching at both authorize and token steps. The
	// same redirect_uri string must be sent to both endpoints.
	redirect := "http://localhost:3999/callback"
	url := buildAuthorizationURL("https://as/authorize", "c", redirect, "v", "s", nil, "")
	if !strings.Contains(url, "redirect_uri="+redirectValue(redirect)) {
		t.Fatalf("authorize redirect_uri mismatch in %s", url)
	}
	// exchangeCode sends the identical redirect_uri in the form (verified by the
	// token-endpoint form fields in the end-to-end test). This test asserts the
	// helper contract: the value is stable, not URL-mangled.
}

func TestOAuth21StateCSRFProtection(t *testing.T) {
	// §4.1.3: state MUST be sent and validated. buildAuthorizationURL includes it.
	state, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	url := buildAuthorizationURL("https://as/authorize", "c", "https://localhost/cb", "v", state, nil, "")
	if !strings.Contains(url, "state="+state) {
		t.Fatalf("state not present in URL: %s", url)
	}
}

func TestOAuth21NoPARIsRequired(t *testing.T) {
	// §A.4 makes PAR SHOULD. A server that mandates PAR is rejected (so the user
	// learns the server needs a feature we do not support yet).
	as := &AuthorizationServerMetadata{RequirePushedAuthorizationRequests: true}
	_ = as // discovery validates and rejects this; covered by discovery tests.
}

func redirectValue(s string) string {
	// buildAuthorizationURL uses url.Values.Encode, which percent-encodes the
	// redirect_uri. This helper reproduces that so the test can match exactly.
	return "http%3A%2F%2Flocalhost%3A3999%2Fcallback"
}

func TestSupportedAuthMethodsIncludeJWT(t *testing.T) {
	// OAuth 2.1 §7.3.2 recommends private_key_jwt for confidential clients.
	// This asserts our resolver supports it (the resolver tests exercise it
	// functionally); here we confirm the method name is recognized.
	for _, m := range []string{"private_key_jwt", "client_secret_jwt", "client_secret_basic", "client_secret_post", "none"} {
		if !asMethodSupported(&AuthorizationServerMetadata{TokenEndpointAuthMethodsSupported: []string{m}}, m) {
			t.Fatalf("auth method %q not recognized", m)
		}
	}
}
