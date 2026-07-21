package mcpauth

import (
	"crypto"
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
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"
)

// JWT assertion lifetimes per RFC 7523 §2.2 / §3. The assertion is short-lived
// and single-use (jti guards replay); a few minutes is plenty.
const (
	assertionTTL     = 5 * time.Minute
	assertionSkew    = 30 * time.Second
	assertionIDBytes = 16
)

// supportedJWSAlgs is the JOSE JWS signature algorithm set this client can sign.
// It covers every algorithm a modern authorization server is likely to request
// for JWT client authentication or assertion grants, implemented with the Go
// standard library only.
var supportedJWSAlgs = map[string]bool{
	"RS256": true, "RS384": true, "RS512": true, // RSASSA-PKCS1-v1_5
	"PS256": true, "PS384": true, "PS512": true, // RSASSA-PSS
	"ES256": true, "ES384": true, "ES512": true, // ECDSA
	"HS256": true, "HS384": true, "HS512": true, // HMAC
	"EdDSA": true, // Ed25519
}

// hashFor returns the crypto.Hash an algorithm requires, or an error when the
// algorithm is unsupported. Ed25519 signs the message directly (no prehash).
func hashFor(alg string) (crypto.Hash, error) {
	switch alg {
	case "RS256", "PS256", "ES256", "HS256":
		return crypto.SHA256, nil
	case "RS384", "PS384", "ES384", "HS384":
		return crypto.SHA384, nil
	case "RS512", "PS512", "ES512", "HS512":
		return crypto.SHA512, nil
	case "EdDSA":
		return crypto.Hash(0), nil
	default:
		return 0, fmt.Errorf("unsupported JWS algorithm %q", alg)
	}
}

// signJWT builds a compact-serialization JWS (header.payload.signature) signed
// with key under alg. key must match the algorithm family: *rsa.PrivateKey for
// RS*/PS*, *ecdsa.PrivateKey for ES*, ed25519.PrivateKey for EdDSA, and []byte
// (secret) for HS*. claims is marshaled to JSON as the payload.
func signJWT(claims map[string]any, key crypto.PrivateKey, alg string) (string, error) {
	if !supportedJWSAlgs[alg] {
		return "", fmt.Errorf("unsupported JWS algorithm %q", alg)
	}
	header := map[string]string{"alg": alg, "typ": "JWT"}
	headerB, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadB, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerB) + "." +
		base64.RawURLEncoding.EncodeToString(payloadB)

	sig, err := jwsSign([]byte(signingInput), key, alg)
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// jwsSign produces the JWS signature bytes for signingInput. The private key
// type is matched to the algorithm family; a mismatch is an error rather than a
// silent wrong-key signature.
func jwsSign(signingInput []byte, key crypto.PrivateKey, alg string) ([]byte, error) {
	switch alg {
	case "RS256", "RS384", "RS512":
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s requires an *rsa.PrivateKey", alg)
		}
		h, err := hashFor(alg)
		if err != nil {
			return nil, err
		}
		hh := h.New()
		hh.Write(signingInput)
		return rsa.SignPKCS1v15(rand.Reader, rk, h, hh.Sum(nil))
	case "PS256", "PS384", "PS512":
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s requires an *rsa.PrivateKey", alg)
		}
		h, err := hashFor(alg)
		if err != nil {
			return nil, err
		}
		hh := h.New()
		hh.Write(signingInput)
		return rsa.SignPSS(rand.Reader, rk, h, hh.Sum(nil), &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	case "ES256", "ES384", "ES512":
		ek, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s requires an *ecdsa.PrivateKey", alg)
		}
		h, err := hashFor(alg)
		if err != nil {
			return nil, err
		}
		hh := h.New()
		hh.Write(signingInput)
		r, s, err := ecdsa.Sign(rand.Reader, ek, hh.Sum(nil))
		if err != nil {
			return nil, err
		}
		return encodeECDSASig(r, s, ecdsaKeySize(ek.Curve)), nil
	case "HS256", "HS384", "HS512":
		secret, ok := key.([]byte)
		if !ok {
			return nil, fmt.Errorf("%s requires a []byte secret", alg)
		}
		return hmacSign(signingInput, secret, alg)
	case "EdDSA":
		ed, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("EdDSA requires an ed25519.PrivateKey")
		}
		sig, err := ed.Sign(rand.Reader, signingInput, crypto.Hash(0))
		return sig, err
	default:
		return nil, fmt.Errorf("unsupported JWS algorithm %q", alg)
	}
}

// hmacSign computes the HMAC for HS* algorithms. hmac.New wants a
// func() hash.Hash; crypto.Hash.New matches that exactly.
func hmacSign(signingInput, secret []byte, alg string) ([]byte, error) {
	h, err := hashFor(alg)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(func() hash.Hash { return h.New() }, secret)
	mac.Write(signingInput)
	return mac.Sum(nil), nil
}

// encodeECDSASig produces the fixed-size R||S concatenation JWS requires for
// ECDSA signatures (RFC 7515 §3.4), padding each integer to the curve's byte
// size. keySize is the byte length of the curve order.
func encodeECDSASig(r, s *big.Int, keySize int) []byte {
	out := make([]byte, 2*keySize)
	r.FillBytes(out[:keySize])
	s.FillBytes(out[keySize:])
	return out
}

// ecdsaKeySize returns the fixed byte length of the curve's base point order,
// used to pad ECDSA signature halves for JWS.
func ecdsaKeySize(c elliptic.Curve) int {
	return (c.Params().BitSize + 7) / 8
}

// parsePrivateKey decodes a PEM-encoded private key (PKCS#8, PKCS#1, or SEC 1)
// into a crypto.PrivateKey. The block may be of type "PRIVATE KEY" (PKCS#8),
// "RSA PRIVATE KEY" (PKCS#1), or "EC PRIVATE KEY" (SEC 1).
func parsePrivateKey(pemData []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("no PEM block found in private key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY", "ENCRYPTED PRIVATE KEY":
		// PKCS#8 (unencrypted only; encrypted keys are out of scope — callers
		// should store unencrypted keys in a 0600 file or supply via env).
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM key type %q", block.Type)
	}
}

// defaultAlgFor picks a sensible JWS algorithm for a private key type when the
// caller does not specify one.
func defaultAlgFor(key crypto.PrivateKey) (string, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return "RS256", nil
	case *ecdsa.PrivateKey:
		switch k.Curve {
		case elliptic.P256():
			return "ES256", nil
		case elliptic.P384():
			return "ES384", nil
		case elliptic.P521():
			return "ES512", nil
		default:
			return "", fmt.Errorf("unsupported ECDSA curve %s", k.Curve.Params().Name)
		}
	case ed25519.PrivateKey:
		return "EdDSA", nil
	case []byte:
		return "HS256", nil
	default:
		return "", fmt.Errorf("no default algorithm for key type %T", key)
	}
}

// jwtNow returns the current time; defined as a variable so tests can pin it.
var jwtNow = time.Now

// assertionClaims builds the standard JWT claim set for an RFC 7523 assertion:
// iss/sub identify the client, aud is the token endpoint (or issuer), and
// jti+exp make the assertion single-use and short-lived.
func assertionClaims(subject, audience string) map[string]any {
	now := jwtNow()
	jti, _ := randBytes(assertionIDBytes)
	return map[string]any{
		"iss": subject,
		"sub": subject,
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(assertionTTL).Unix(),
		"nbf": now.Add(-assertionSkew).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti),
	}
}

// randBytes returns n cryptographically random bytes.
func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// algSupportsKey reports whether key can sign with alg (type-family match).
func algSupportsKey(alg string, key crypto.PrivateKey) bool {
	switch alg {
	case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512":
		_, ok := key.(*rsa.PrivateKey)
		return ok
	case "ES256", "ES384", "ES512":
		_, ok := key.(*ecdsa.PrivateKey)
		return ok
	case "EdDSA":
		_, ok := key.(ed25519.PrivateKey)
		return ok
	case "HS256", "HS384", "HS512":
		_, ok := key.([]byte)
		return ok
	default:
		return false
	}
}

// splitJWT returns the three compact-serialization parts of a JWT for inspection
// (used by tests; not used in the production flow).
func splitJWT(token string) (header, payload, signature string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
