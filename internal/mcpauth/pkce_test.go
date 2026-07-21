package mcpauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateCodeVerifierLengthAndCharset(t *testing.T) {
	v, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Fatalf("verifier length %d outside RFC 7636 range [43,128]", len(v))
	}
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	for _, r := range v {
		if !strings.ContainsRune(unreserved, r) {
			t.Fatalf("verifier contains non-unreserved char %q", r)
		}
	}
}

func TestCodeVerifierUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		v, err := generateCodeVerifier()
		if err != nil {
			t.Fatal(err)
		}
		if seen[v] {
			t.Fatalf("duplicate verifier generated: %s", v)
		}
		seen[v] = true
	}
}

func TestCodeChallengeS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// Known answer from RFC 7636 Appendix B.
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := codeChallengeS256(verifier); got != want {
		t.Fatalf("codeChallengeS256 = %q, want %q", got, want)
	}
	// Re-derive independently to be sure it matches the spec formula.
	sum := sha256.Sum256([]byte(verifier))
	manual := base64.RawURLEncoding.EncodeToString(sum[:])
	if manual != want {
		t.Fatalf("manual S256 mismatch")
	}
}

func TestAuthorizationServerMetadataSupportsS256(t *testing.T) {
	cases := []struct {
		name string
		m    *AuthorizationServerMetadata
		want bool
	}{
		{"nil", nil, true},
		{"empty-list", &AuthorizationServerMetadata{}, true},
		{"plain-only", &AuthorizationServerMetadata{CodeChallengeMethodsSupported: []string{"plain"}}, false},
		{"s256", &AuthorizationServerMetadata{CodeChallengeMethodsSupported: []string{"plain", "S256"}}, true},
		{"s256-lower", &AuthorizationServerMetadata{CodeChallengeMethodsSupported: []string{"s256"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.supportsS256(); got != tc.want {
				t.Fatalf("supportsS256 = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAuthorizationServerMetadataSupportsAuthCode(t *testing.T) {
	if !(&AuthorizationServerMetadata{}).supportsAuthorizationCode() {
		t.Fatal("empty metadata should permit authorization_code")
	}
	noCode := &AuthorizationServerMetadata{GrantTypesSupported: []string{"client_credentials"}}
	if noCode.supportsAuthorizationCode() {
		t.Fatal("client_credentials-only AS should not support authorization_code")
	}
}
