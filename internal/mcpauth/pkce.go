package mcpauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// codeVerifierBytes is the amount of randomness used to build the PKCE code
// verifier. base64url of 32 bytes yields 43 chars, the minimum allowed by
// RFC 7636 §4.1; we use 48 (64 chars) for extra margin.
const codeVerifierBytes = 48

// generateCodeVerifier returns a high-entropy code verifier over the unreserved
// URI character set (A-Z a-z 0-9 - _ . ~), suitable for RFC 7636 PKCE.
func generateCodeVerifier() (string, error) {
	b := make([]byte, codeVerifierBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// codeChallengeS256 computes the S256 code challenge for a verifier per
// RFC 7636 §4.2: BASE64URL-ENCODE(SHA256(ASCII(verifier))).
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// randomState returns an opaque CSRF token echoed back on the redirect.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
