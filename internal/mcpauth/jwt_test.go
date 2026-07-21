package mcpauth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
)

func TestJWTSignsAndVerifiesAllAlgorithms(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	hmacSecret := []byte("test-secret")

	cases := []struct {
		name string
		alg  string
		key  any
	}{
		{"RS256", "RS256", rsaKey},
		{"RS384", "RS384", rsaKey},
		{"RS512", "RS512", rsaKey},
		{"PS256", "PS256", rsaKey},
		{"ES256", "ES256", mustGenEC(t, elliptic.P256())},
		{"ES384", "ES384", mustGenEC(t, elliptic.P384())},
		{"ES512", "ES512", mustGenEC(t, elliptic.P521())},
		{"HS256", "HS256", hmacSecret},
		{"HS384", "HS384", hmacSecret},
		{"HS512", "HS512", hmacSecret},
		{"EdDSA", "EdDSA", mustGenEd(t)},
	}
	claims := map[string]any{"sub": "client-1", "aud": "https://as/token", "jti": "x"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := signJWT(claims, tc.key, tc.alg)
			if err != nil {
				t.Fatalf("signJWT: %v", err)
			}
			header, payload, sig, ok := splitJWT(token)
			if !ok {
				t.Fatal("malformed JWT")
			}
			// Header carries alg + typ.
			hb, _ := base64.RawURLEncoding.DecodeString(header)
			var hdr struct {
				Alg string `json:"alg"`
				Typ string `json:"typ"`
			}
			if err := json.Unmarshal(hb, &hdr); err != nil || hdr.Alg != tc.alg || hdr.Typ != "JWT" {
				t.Fatalf("header = %s", hb)
			}
			// Payload round-trips.
			pb, _ := base64.RawURLEncoding.DecodeString(payload)
			var got map[string]any
			if err := json.Unmarshal(pb, &got); err != nil || got["sub"] != "client-1" {
				t.Fatalf("payload = %s", pb)
			}
			sigBytes, _ := base64.RawURLEncoding.DecodeString(sig)
			signingInput := header + "." + payload
			verifyJWSSimple(t, []byte(signingInput), sigBytes, tc.key, tc.alg)
		})
	}
}

// verifyJWSSimple is a reference verifier for tests, reimplementing only the
// verification half of each algorithm against the signer under test.
func verifyJWSSimple(t *testing.T, signingInput, sig []byte, key any, alg string) {
	t.Helper()
	switch alg {
	case "RS256", "RS384", "RS512":
		rk := key.(*rsa.PrivateKey).Public().(*rsa.PublicKey)
		h, _ := hashFor(alg)
		hh := h.New()
		hh.Write(signingInput)
		if err := rsa.VerifyPKCS1v15(rk, h, hh.Sum(nil), sig); err != nil {
			t.Fatalf("RSA verify: %v", err)
		}
	case "PS256", "PS384", "PS512":
		rk := key.(*rsa.PrivateKey).Public().(*rsa.PublicKey)
		h, _ := hashFor(alg)
		hh := h.New()
		hh.Write(signingInput)
		if err := rsa.VerifyPSS(rk, h, hh.Sum(nil), sig, nil); err != nil {
			t.Fatalf("RSA-PSS verify: %v", err)
		}
	case "ES256", "ES384", "ES512":
		ek := key.(*ecdsa.PrivateKey).Public().(*ecdsa.PublicKey)
		h, _ := hashFor(alg)
		hh := h.New()
		hh.Write(signingInput)
		size := (ek.Curve.Params().BitSize + 7) / 8
		r := new(big.Int).SetBytes(sig[:size])
		s := new(big.Int).SetBytes(sig[size:])
		if !ecdsa.Verify(ek, hh.Sum(nil), r, s) {
			t.Fatal("ECDSA verify failed")
		}
	case "HS256", "HS384", "HS512":
		secret := key.([]byte)
		h, _ := hashFor(alg)
		mac := hmac.New(h.New, secret)
		mac.Write(signingInput)
		if !hmac.Equal(mac.Sum(nil), sig) {
			t.Fatal("HMAC mismatch")
		}
	case "EdDSA":
		pk := key.(ed25519.PrivateKey).Public().(ed25519.PublicKey)
		if !ed25519.Verify(pk, signingInput, sig) {
			t.Fatal("Ed25519 verify failed")
		}
	}
}

func mustGenEC(t *testing.T, c elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(c, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustGenEd(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestSignJWTRejectsKeyAlgorithmMismatch(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	// ES256 needs an EC key; passing an RSA key must error, not silently mis-sign.
	if _, err := signJWT(map[string]any{"sub": "x"}, rsaKey, "ES256"); err == nil {
		t.Fatal("expected error signing ES256 with an RSA key")
	}
	// RS256 needs an RSA key; passing a []byte must error.
	if _, err := signJWT(map[string]any{"sub": "x"}, []byte("secret"), "RS256"); err == nil {
		t.Fatal("expected error signing RS256 with an HMAC secret")
	}
}

func TestSignJWTRejectsUnsupportedAlg(t *testing.T) {
	if _, err := signJWT(map[string]any{"sub": "x"}, []byte("k"), "none"); err == nil {
		t.Fatal("expected error for alg=none")
	}
}

func TestParsePrivateKeyHandlesPEMFormats(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	// PKCS#1 (RSA PRIVATE KEY).
	rsa1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	if _, err := parsePrivateKey(rsa1); err != nil {
		t.Fatalf("PKCS1 RSA: %v", err)
	}
	// PKCS#8 (PRIVATE KEY).
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	rsa8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	if _, err := parsePrivateKey(rsa8); err != nil {
		t.Fatalf("PKCS8 RSA: %v", err)
	}
	// SEC 1 (EC PRIVATE KEY).
	ecDER, _ := x509.MarshalECPrivateKey(ecKey)
	ec1 := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})
	if _, err := parsePrivateKey(ec1); err != nil {
		t.Fatalf("SEC1 EC: %v", err)
	}
	// Garbage fails cleanly.
	if _, err := parsePrivateKey([]byte("not pem")); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func TestDefaultAlgForKeyType(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, edPriv, _ := ed25519.GenerateKey(rand.Reader)

	cases := map[string]string{
		"rsa":  "RS256",
		"ec":   "ES256",
		"ed":   "EdDSA",
		"hmac": "HS256",
	}
	keys := map[string]any{"rsa": rsaKey, "ec": ecKey, "ed": edPriv, "hmac": []byte("k")}
	for name, want := range cases {
		got, err := defaultAlgFor(keys[name])
		if err != nil || got != want {
			t.Errorf("defaultAlgFor(%s) = %q err=%v, want %q", name, got, err, want)
		}
	}
}

func TestAssertionClaimsShape(t *testing.T) {
	c := assertionClaims("client-1", "https://as/token")
	for _, k := range []string{"iss", "sub", "aud", "iat", "exp", "nbf", "jti"} {
		if _, ok := c[k]; !ok {
			t.Fatalf("assertion claim %q missing", k)
		}
	}
	if c["iss"] != "client-1" || c["sub"] != "client-1" || c["aud"] != "https://as/token" {
		t.Fatalf("iss/sub/aud wrong: %+v", c)
	}
	// jti must be unique across calls (replay protection).
	c2 := assertionClaims("client-1", "https://as/token")
	if c["jti"] == c2["jti"] {
		t.Fatal("jti must differ between assertions")
	}
}

func TestSignedJWTHeaderMatchesAlg(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok, _ := signJWT(map[string]any{"x": 1}, rsaKey, "PS512")
	h, _, _, _ := splitJWT(tok)
	hb, _ := base64.RawURLEncoding.DecodeString(h)
	if !strings.Contains(string(hb), `"alg":"PS512"`) {
		t.Fatalf("header alg mismatch: %s", hb)
	}
}
