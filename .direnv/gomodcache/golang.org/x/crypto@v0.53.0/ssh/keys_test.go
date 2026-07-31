// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"bytes"
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/testdata"
)

func rawKey(pub PublicKey) interface{} {
	switch k := pub.(type) {
	case *rsaPublicKey:
		return (*rsa.PublicKey)(k)
	case *dsaPublicKey:
		return (*dsa.PublicKey)(k)
	case *ecdsaPublicKey:
		return (*ecdsa.PublicKey)(k)
	case ed25519PublicKey:
		return (ed25519.PublicKey)(k)
	case *Certificate:
		return k
	}
	panic("unknown key type")
}

func TestKeyMarshalParse(t *testing.T) {
	for _, priv := range testSigners {
		pub := priv.PublicKey()
		roundtrip, err := ParsePublicKey(pub.Marshal())
		if err != nil {
			t.Errorf("ParsePublicKey(%T): %v", pub, err)
		}

		k1 := rawKey(pub)
		k2 := rawKey(roundtrip)

		if !reflect.DeepEqual(k1, k2) {
			t.Errorf("got %#v in roundtrip, want %#v", k2, k1)
		}
	}
}

func TestParsePublicKeyWithSigningAlgoAsKeyFormat(t *testing.T) {
	key := []byte(`rsa-sha2-256 AAAADHJzYS1zaGEyLTI1NgAAAAMBAAEAAAEBAJ7qMyjLXEJCCJmRknuCLo0uPi5GrPY5pQYr84lhlN8Gor5KVL2LKYCW4e70r5xzj7SrHHSCft1FMlYg1KDO9xrprJh733kQqAPWETmSuH0EfRtGtcH6EarKyVxk6As076/yNiiMKVBtG0RPa1L7FviTfcYK4vnCCVrbv3RmA5CCzuG5BSMbRLxzVb4Ri3p8jhxYT8N4QGe/2yqvJLys5vQ9szpZR3tcFp3DJIVZhBRfR6LnoY23XZniAAMQaUVBX86dXQ++dNwAwZSXSt9Og+AniOCiBYqhNVa5n3DID/H7YtEtG+CbZr3r2KD3fv8AfSLRar4XOp8rsRdD31h/kr8=`)
	_, _, _, _, err := ParseAuthorizedKey(key)
	if err == nil {
		t.Fatal("parsing a public key using a signature algorithm as the key format succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), `signature algorithm "rsa-sha2-256" isn't a key format`) {
		t.Errorf(`got %v, expected 'signature algorithm "rsa-sha2-256" isn't a key format'`, err)
	}
}

func TestUnsupportedCurves(t *testing.T) {
	raw, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	if _, err = NewSignerFromKey(raw); err == nil || !strings.Contains(err.Error(), "only P-256") {
		t.Fatalf("NewPrivateKey should not succeed with P-224, got: %v", err)
	}

	if _, err = NewPublicKey(&raw.PublicKey); err == nil || !strings.Contains(err.Error(), "only P-256") {
		t.Fatalf("NewPublicKey should not succeed with P-224, got: %v", err)
	}
}

func TestNewPublicKey(t *testing.T) {
	for _, k := range testSigners {
		raw := rawKey(k.PublicKey())
		// Skip certificates, as NewPublicKey does not support them.
		if _, ok := raw.(*Certificate); ok {
			continue
		}
		pub, err := NewPublicKey(raw)
		if err != nil {
			t.Errorf("NewPublicKey(%#v): %v", raw, err)
		}
		if !reflect.DeepEqual(k.PublicKey(), pub) {
			t.Errorf("NewPublicKey(%#v) = %#v, want %#v", raw, pub, k.PublicKey())
		}
	}
}

func TestKeySignVerify(t *testing.T) {
	for _, priv := range testSigners {
		pub := priv.PublicKey()

		data := []byte("sign me")
		sig, err := priv.Sign(rand.Reader, data)
		if err != nil {
			t.Fatalf("Sign(%T): %v", priv, err)
		}

		if err := pub.Verify(data, sig); err != nil {
			t.Errorf("publicKey.Verify(%T): %v", priv, err)
		}
		sig.Blob[5]++
		if err := pub.Verify(data, sig); err == nil {
			t.Errorf("publicKey.Verify on broken sig did not fail")
		}
	}
}

func TestKeySignWithAlgorithmVerify(t *testing.T) {
	for k, priv := range testSigners {
		if algorithmSigner, ok := priv.(MultiAlgorithmSigner); !ok {
			t.Errorf("Signers %q constructed by ssh package should always implement the MultiAlgorithmSigner interface: %T", k, priv)
		} else {
			pub := priv.PublicKey()
			data := []byte("sign me")

			signWithAlgTestCase := func(algorithm string, expectedAlg string) {
				sig, err := algorithmSigner.SignWithAlgorithm(rand.Reader, data, algorithm)
				if err != nil {
					t.Fatalf("Sign(%T): %v", priv, err)
				}
				if sig.Format != expectedAlg {
					t.Errorf("signature format did not match requested signature algorithm: %s != %s", sig.Format, expectedAlg)
				}

				if err := pub.Verify(data, sig); err != nil {
					t.Errorf("publicKey.Verify(%T): %v", priv, err)
				}
				sig.Blob[5]++
				if err := pub.Verify(data, sig); err == nil {
					t.Errorf("publicKey.Verify on broken sig did not fail")
				}
			}

			// Using the empty string as the algorithm name should result in the same signature format as the algorithm-free Sign method.
			defaultSig, err := priv.Sign(rand.Reader, data)
			if err != nil {
				t.Fatalf("Sign(%T): %v", priv, err)
			}
			signWithAlgTestCase("", defaultSig.Format)

			// RSA keys are the only ones which currently support more than one signing algorithm
			if pub.Type() == KeyAlgoRSA {
				for _, algorithm := range []string{KeyAlgoRSA, KeyAlgoRSASHA256, KeyAlgoRSASHA512} {
					signWithAlgTestCase(algorithm, algorithm)
				}
			}
		}
	}
}

func TestKeySignWithShortSignature(t *testing.T) {
	signer := testSigners["rsa"].(AlgorithmSigner)
	pub := signer.PublicKey()
	// Note: data obtained by empirically trying until a result
	// starting with 0 appeared
	tests := []struct {
		algorithm string
		data      []byte
	}{
		{
			algorithm: KeyAlgoRSA,
			data:      []byte("sign me92"),
		},
		{
			algorithm: KeyAlgoRSASHA256,
			data:      []byte("sign me294"),
		},
		{
			algorithm: KeyAlgoRSASHA512,
			data:      []byte("sign me60"),
		},
	}

	for _, tt := range tests {
		sig, err := signer.SignWithAlgorithm(rand.Reader, tt.data, tt.algorithm)
		if err != nil {
			t.Fatalf("Sign(%T): %v", signer, err)
		}
		if sig.Blob[0] != 0 {
			t.Errorf("%s: Expected signature with a leading 0", tt.algorithm)
		}
		sig.Blob = sig.Blob[1:]
		if err := pub.Verify(tt.data, sig); err != nil {
			t.Errorf("publicKey.Verify(%s): %v", tt.algorithm, err)
		}
	}
}

func TestParseRSAPrivateKey(t *testing.T) {
	key := testPrivateKeys["rsa"]

	rsa, ok := key.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("got %T, want *rsa.PrivateKey", rsa)
	}

	if err := rsa.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestParseRSAModulusTooLarge(t *testing.T) {
	rsa16384 := "ssh-rsa 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"
	_, _, _, _, err := ParseAuthorizedKey([]byte(rsa16384))

	if err == nil {
		t.Fatal("ParseAuthorizedKey accepted a 16384-bit modulus; expected it to be rejected")
	}

	expectedError := "rsa modulus too large"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("unexpected error message: got %q, want substring %q", err.Error(), expectedError)
	}
}

func TestParsePrivateKeyRSAModulusTooLarge(t *testing.T) {
	rsa16384 := []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
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
QHAxNnM=
-----END OPENSSH PRIVATE KEY-----
`)
	_, err := ParseRawPrivateKey(rsa16384)
	if err == nil {
		t.Fatal("ParseRawPrivateKey accepted a 16384-bit modulus; expected it to be rejected")
	}

	expectedError := "rsa modulus too large"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("unexpected error message: got %q, want substring %q", err.Error(), expectedError)
	}
}

func TestParseECPrivateKey(t *testing.T) {
	key := testPrivateKeys["ecdsa"]

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("got %T, want *ecdsa.PrivateKey", ecKey)
	}

	if !validateECPublicKey(ecKey.Curve, ecKey.X, ecKey.Y) {
		t.Fatalf("public key does not validate.")
	}
}

func TestParseEncryptedPrivateKeysWithPassphrase(t *testing.T) {
	data := []byte("sign me")
	for _, tt := range testdata.PEMEncryptedKeys {
		t.Run(tt.Name, func(t *testing.T) {
			_, err := ParsePrivateKeyWithPassphrase(tt.PEMBytes, []byte("incorrect"))
			if err != x509.IncorrectPasswordError {
				t.Errorf("got %v want IncorrectPasswordError", err)
			}

			s, err := ParsePrivateKeyWithPassphrase(tt.PEMBytes, []byte(tt.EncryptionKey))
			if err != nil {
				t.Fatalf("ParsePrivateKeyWithPassphrase returned error: %s", err)
			}

			sig, err := s.Sign(rand.Reader, data)
			if err != nil {
				t.Fatalf("Signer.Sign: %v", err)
			}
			if err := s.PublicKey().Verify(data, sig); err != nil {
				t.Errorf("Verify failed: %v", err)
			}

			_, err = ParsePrivateKey(tt.PEMBytes)
			if err == nil {
				t.Fatalf("ParsePrivateKey succeeded, expected an error")
			}

			if err, ok := err.(*PassphraseMissingError); !ok {
				t.Errorf("got error %q, want PassphraseMissingError", err)
			} else if tt.IncludesPublicKey {
				if err.PublicKey == nil {
					t.Fatalf("expected PassphraseMissingError.PublicKey not to be nil")
				}
				got, want := err.PublicKey.Marshal(), s.PublicKey().Marshal()
				if !bytes.Equal(got, want) {
					t.Errorf("error field %q doesn't match signer public key %q", got, want)
				}
			}
		})
	}
}

func TestParseEncryptedPrivateKeysWithUnsupportedCiphers(t *testing.T) {
	for _, tt := range testdata.UnsupportedCipherData {
		t.Run(tt.Name, func(t *testing.T) {
			_, err := ParsePrivateKeyWithPassphrase(tt.PEMBytes, []byte(tt.EncryptionKey))
			if err == nil {
				t.Fatalf("expected 'unknown cipher' error for %q, got nil", tt.Name)
				// If this cipher is now supported, remove it from testdata.UnsupportedCipherData
			}
			if !strings.Contains(err.Error(), "unknown cipher") {
				t.Errorf("wanted 'unknown cipher' error, got %v", err.Error())
			}
		})
	}
}

func TestParseEncryptedPrivateKeysWithIncorrectPassphrase(t *testing.T) {
	pem := testdata.PEMEncryptedKeys[0].PEMBytes
	for i := 0; i < 4096; i++ {
		_, err := ParseRawPrivateKeyWithPassphrase(pem, []byte(fmt.Sprintf("%d", i)))
		if !errors.Is(err, x509.IncorrectPasswordError) {
			t.Fatalf("expected error: %v, got: %v", x509.IncorrectPasswordError, err)
		}
	}
}

func TestParseEncryptedPrivateKeyExcessiveBcryptRounds(t *testing.T) {
	// Craft a minimal openssh-key-v1 blob whose KdfOpts declares a bcrypt
	// round count above the accepted maximum. The check must reject the file
	// before bcrypt_pbkdf is invoked, so the rest of the blob (public key,
	// encrypted body) can be empty.
	kdfOpts := Marshal(struct {
		Salt   []byte
		Rounds uint32
	}{
		Salt:   []byte("salt-not-used"),
		Rounds: (1 << 11) + 1,
	})
	header := Marshal(openSSHEncryptedPrivateKey{
		CipherName: "aes256-ctr",
		KdfName:    "bcrypt",
		KdfOpts:    string(kdfOpts),
		NumKeys:    1,
	})
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: append([]byte(privateKeyAuthMagic), header...),
	})

	_, err := ParseRawPrivateKeyWithPassphrase(pemBytes, []byte("password"))
	if err == nil {
		t.Fatal("expected error for excessive bcrypt rounds, got nil")
	}
	if !strings.Contains(err.Error(), "bcrypt KDF rounds") {
		t.Errorf("got error %q, want substring %q", err.Error(), "bcrypt KDF rounds")
	}
}

func TestParseDSA(t *testing.T) {
	// We actually exercise the ParsePrivateKey codepath here, as opposed to
	// using the ParseRawPrivateKey+NewSignerFromKey path that testdata_test.go
	// uses.
	s, err := ParsePrivateKey(testdata.PEMBytes["dsa"])
	if err != nil {
		t.Fatalf("ParsePrivateKey returned error: %s", err)
	}

	data := []byte("sign me")
	sig, err := s.Sign(rand.Reader, data)
	if err != nil {
		t.Fatalf("dsa.Sign: %v", err)
	}

	if err := s.PublicKey().Verify(data, sig); err != nil {
		t.Errorf("Verify failed: %v", err)
	}
}

// Tests for authorized_keys parsing.

// getTestKey returns a public key, and its base64 encoding.
func getTestKey() (PublicKey, string) {
	k := testPublicKeys["rsa"]

	b := &bytes.Buffer{}
	e := base64.NewEncoder(base64.StdEncoding, b)
	e.Write(k.Marshal())
	e.Close()

	return k, b.String()
}

func TestMarshalParsePublicKey(t *testing.T) {
	pub, pubSerialized := getTestKey()
	line := fmt.Sprintf("%s %s user@host", pub.Type(), pubSerialized)

	authKeys := MarshalAuthorizedKey(pub)
	actualFields := strings.Fields(string(authKeys))
	if len(actualFields) == 0 {
		t.Fatalf("failed authKeys: %v", authKeys)
	}

	// drop the comment
	expectedFields := strings.Fields(line)[0:2]

	if !reflect.DeepEqual(actualFields, expectedFields) {
		t.Errorf("got %v, expected %v", actualFields, expectedFields)
	}

	actPub, _, _, _, err := ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatalf("cannot parse %v: %v", line, err)
	}
	if !reflect.DeepEqual(actPub, pub) {
		t.Errorf("got %v, expected %v", actPub, pub)
	}
}

func TestParseDSAHugeQ(t *testing.T) {
	P := new(big.Int).Lsh(big.NewInt(1), 1023)
	Q := new(big.Int).Lsh(big.NewInt(1), 20000) // very large
	// G and Y: Dummy values, just needs to be < P to pass that specific check
	G := big.NewInt(2)
	Y := big.NewInt(5)

	rawKey := struct {
		P, Q, G, Y *big.Int
	}{
		P: P,
		Q: Q,
		G: G,
		Y: Y,
	}

	inputBytes := Marshal(&rawKey)

	_, _, err := parseDSA(inputBytes)
	if err == nil {
		t.Fatal("parseDSA accepted a DSA key with large Q")
	}

	expectedError := "ssh: unsupported DSA sub-prime size"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("unexpected error message: got %q, want substring %q", err.Error(), expectedError)
	}
}

func TestParseDSAYOutOfRange(t *testing.T) {
	// Valid 1024/160 parameters (values don't need to be a real DSA group,
	// they only need to pass the checkDSAParams bit-length checks and the
	// G < P / G > 0 checks).
	P := new(big.Int).Lsh(big.NewInt(1), 1023)
	P.SetBit(P, 0, 1) // make P odd so it can pass as a prime candidate shape
	Q := new(big.Int).Lsh(big.NewInt(1), 159)
	Q.SetBit(Q, 0, 1)
	G := big.NewInt(2)

	for _, tc := range []struct {
		name string
		Y    *big.Int
	}{
		{"Y_zero", big.NewInt(0)},
		{"Y_negative", big.NewInt(-1)},
		{"Y_equals_P", new(big.Int).Set(P)},
		{"Y_greater_than_P", new(big.Int).Add(P, big.NewInt(1))},
		{"Y_much_greater_than_P", new(big.Int).Lsh(big.NewInt(1), 20000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rawKey := struct {
				P, Q, G, Y *big.Int
			}{P: P, Q: Q, G: G, Y: tc.Y}

			_, _, err := parseDSA(Marshal(&rawKey))
			if err == nil {
				t.Fatalf("parseDSA accepted a DSA key with Y=%s (P=%s)", tc.Y, P)
			}
			expectedError := "DSA public value Y out of range"
			if !strings.Contains(err.Error(), expectedError) {
				t.Errorf("unexpected error message: got %q, want substring %q", err.Error(), expectedError)
			}
		})
	}
}

func TestMarshalPrivateKey(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"rsa-openssh-format"},
		{"ed25519"},
		{"p256-openssh-format"},
		{"p384-openssh-format"},
		{"p521-openssh-format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected, ok := testPrivateKeys[tt.name]
			if !ok {
				t.Fatalf("cannot find key %s", tt.name)
			}

			block, err := MarshalPrivateKey(expected, "test@golang.org")
			if err != nil {
				t.Fatalf("cannot marshal %s: %v", tt.name, err)
			}

			key, err := ParseRawPrivateKey(pem.EncodeToMemory(block))
			if err != nil {
				t.Fatalf("cannot parse %s: %v", tt.name, err)
			}

			if !reflect.DeepEqual(expected, key) {
				t.Errorf("unexpected marshaled key %s", tt.name)
			}
		})
	}
}

func TestMarshalPrivateKeyWithPassphrase(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"rsa-openssh-format"},
		{"ed25519"},
		{"p256-openssh-format"},
		{"p384-openssh-format"},
		{"p521-openssh-format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected, ok := testPrivateKeys[tt.name]
			if !ok {
				t.Fatalf("cannot find key %s", tt.name)
			}

			block, err := MarshalPrivateKeyWithPassphrase(expected, "test@golang.org", []byte("test-passphrase"))
			if err != nil {
				t.Fatalf("cannot marshal %s: %v", tt.name, err)
			}

			key, err := ParseRawPrivateKeyWithPassphrase(pem.EncodeToMemory(block), []byte("test-passphrase"))
			if err != nil {
				t.Fatalf("cannot parse %s: %v", tt.name, err)
			}

			if !reflect.DeepEqual(expected, key) {
				t.Errorf("unexpected marshaled key %s", tt.name)
			}
		})
	}
}

type testAuthResult struct {
	pubKey   PublicKey
	options  []string
	comments string
	rest     string
	ok       bool
}

func testAuthorizedKeys(t *testing.T, authKeys []byte, expected []testAuthResult) {
	rest := authKeys
	var values []testAuthResult
	for len(rest) > 0 {
		var r testAuthResult
		var err error
		r.pubKey, r.comments, r.options, rest, err = ParseAuthorizedKey(rest)
		r.ok = (err == nil)
		t.Log(err)
		r.rest = string(rest)
		values = append(values, r)
	}

	if !reflect.DeepEqual(values, expected) {
		t.Errorf("got %#v, expected %#v", values, expected)
	}
}

func TestAuthorizedKeyBasic(t *testing.T) {
	pub, pubSerialized := getTestKey()
	line := "ssh-rsa " + pubSerialized + " user@host"
	testAuthorizedKeys(t, []byte(line),
		[]testAuthResult{
			{pub, nil, "user@host", "", true},
		})
}

func TestAuth(t *testing.T) {
	pub, pubSerialized := getTestKey()
	authWithOptions := []string{
		`# comments to ignore before any keys...`,
		``,
		`env="HOME=/home/root",no-port-forwarding ssh-rsa ` + pubSerialized + ` user@host`,
		`# comments to ignore, along with a blank line`,
		``,
		`env="HOME=/home/root2" ssh-rsa ` + pubSerialized + ` user2@host2`,
		``,
		`# more comments, plus a invalid entry`,
		`ssh-rsa data-that-will-not-parse user@host3`,
	}
	for _, eol := range []string{"\n", "\r\n"} {
		authOptions := strings.Join(authWithOptions, eol)
		rest2 := strings.Join(authWithOptions[3:], eol)
		rest3 := strings.Join(authWithOptions[6:], eol)
		testAuthorizedKeys(t, []byte(authOptions), []testAuthResult{
			{pub, []string{`env="HOME=/home/root"`, "no-port-forwarding"}, "user@host", rest2, true},
			{pub, []string{`env="HOME=/home/root2"`}, "user2@host2", rest3, true},
			{nil, nil, "", "", false},
		})
	}
}

func TestAuthWithQuotedSpaceInEnv(t *testing.T) {
	pub, pubSerialized := getTestKey()
	authWithQuotedSpaceInEnv := []byte(`env="HOME=/home/root dir",no-port-forwarding ssh-rsa ` + pubSerialized + ` user@host`)
	testAuthorizedKeys(t, []byte(authWithQuotedSpaceInEnv), []testAuthResult{
		{pub, []string{`env="HOME=/home/root dir"`, "no-port-forwarding"}, "user@host", "", true},
	})
}

func TestAuthWithQuotedCommaInEnv(t *testing.T) {
	pub, pubSerialized := getTestKey()
	authWithQuotedCommaInEnv := []byte(`env="HOME=/home/root,dir",no-port-forwarding ssh-rsa ` + pubSerialized + `   user@host`)
	testAuthorizedKeys(t, []byte(authWithQuotedCommaInEnv), []testAuthResult{
		{pub, []string{`env="HOME=/home/root,dir"`, "no-port-forwarding"}, "user@host", "", true},
	})
}

func TestAuthWithQuotedQuoteInEnv(t *testing.T) {
	pub, pubSerialized := getTestKey()
	authWithQuotedQuoteInEnv := []byte(`env="HOME=/home/\"root dir",no-port-forwarding` + "\t" + `ssh-rsa` + "\t" + pubSerialized + `   user@host`)
	authWithDoubleQuotedQuote := []byte(`no-port-forwarding,env="HOME=/home/ \"root dir\"" ssh-rsa ` + pubSerialized + "\t" + `user@host`)
	testAuthorizedKeys(t, []byte(authWithQuotedQuoteInEnv), []testAuthResult{
		{pub, []string{`env="HOME=/home/\"root dir"`, "no-port-forwarding"}, "user@host", "", true},
	})

	testAuthorizedKeys(t, []byte(authWithDoubleQuotedQuote), []testAuthResult{
		{pub, []string{"no-port-forwarding", `env="HOME=/home/ \"root dir\""`}, "user@host", "", true},
	})
}

func TestAuthWithInvalidSpace(t *testing.T) {
	_, pubSerialized := getTestKey()
	authWithInvalidSpace := []byte(`env="HOME=/home/root dir", no-port-forwarding ssh-rsa ` + pubSerialized + ` user@host
#more to follow but still no valid keys`)
	testAuthorizedKeys(t, []byte(authWithInvalidSpace), []testAuthResult{
		{nil, nil, "", "", false},
	})
}

func TestAuthWithMissingQuote(t *testing.T) {
	pub, pubSerialized := getTestKey()
	authWithMissingQuote := []byte(`env="HOME=/home/root,no-port-forwarding ssh-rsa ` + pubSerialized + ` user@host
env="HOME=/home/root",shared-control ssh-rsa ` + pubSerialized + ` user@host`)

	testAuthorizedKeys(t, []byte(authWithMissingQuote), []testAuthResult{
		{pub, []string{`env="HOME=/home/root"`, `shared-control`}, "user@host", "", true},
	})
}

func TestInvalidEntry(t *testing.T) {
	authInvalid := []byte(`ssh-rsa`)
	_, _, _, _, err := ParseAuthorizedKey(authInvalid)
	if err == nil {
		t.Errorf("got valid entry for %q", authInvalid)
	}
}

var knownHostsParseTests = []struct {
	input string
	err   string

	marker  string
	comment string
	hosts   []string
	rest    string
}{
	{
		"",
		"EOF",

		"", "", nil, "",
	},
	{
		"# Just a comment",
		"EOF",

		"", "", nil, "",
	},
	{
		"   \t   ",
		"EOF",

		"", "", nil, "",
	},
	{
		"localhost ssh-rsa {RSAPUB}",
		"",

		"", "", []string{"localhost"}, "",
	},
	{
		"localhost\tssh-rsa {RSAPUB}",
		"",

		"", "", []string{"localhost"}, "",
	},
	{
		"localhost\tssh-rsa {RSAPUB}\tcomment comment",
		"",

		"", "comment comment", []string{"localhost"}, "",
	},
	{
		"localhost\tssh-rsa {RSAPUB}\tcomment comment\n",
		"",

		"", "comment comment", []string{"localhost"}, "",
	},
	{
		"localhost\tssh-rsa {RSAPUB}\tcomment comment\r\n",
		"",

		"", "comment comment", []string{"localhost"}, "",
	},
	{
		"localhost\tssh-rsa {RSAPUB}\tcomment comment\r\nnext line",
		"",

		"", "comment comment", []string{"localhost"}, "next line",
	},
	{
		"localhost,[host2:123]\tssh-rsa {RSAPUB}\tcomment comment",
		"",

		"", "comment comment", []string{"localhost", "[host2:123]"}, "",
	},
	{
		"@marker \tlocalhost,[host2:123]\tssh-rsa {RSAPUB}",
		"",

		"marker", "", []string{"localhost", "[host2:123]"}, "",
	},
	{
		"@marker \tlocalhost,[host2:123]\tssh-rsa aabbccdd",
		"short read",

		"", "", nil, "",
	},
}

func TestKnownHostsParsing(t *testing.T) {
	rsaPub, rsaPubSerialized := getTestKey()

	for i, test := range knownHostsParseTests {
		var expectedKey PublicKey
		const rsaKeyToken = "{RSAPUB}"

		input := test.input
		if strings.Contains(input, rsaKeyToken) {
			expectedKey = rsaPub
			input = strings.Replace(test.input, rsaKeyToken, rsaPubSerialized, -1)
		}

		marker, hosts, pubKey, comment, rest, err := ParseKnownHosts([]byte(input))
		if err != nil {
			if len(test.err) == 0 {
				t.Errorf("#%d: unexpectedly failed with %q", i, err)
			} else if !strings.Contains(err.Error(), test.err) {
				t.Errorf("#%d: expected error containing %q, but got %q", i, test.err, err)
			}
			continue
		} else if len(test.err) != 0 {
			t.Errorf("#%d: succeeded but expected error including %q", i, test.err)
			continue
		}

		if !reflect.DeepEqual(expectedKey, pubKey) {
			t.Errorf("#%d: expected key %#v, but got %#v", i, expectedKey, pubKey)
		}

		if marker != test.marker {
			t.Errorf("#%d: expected marker %q, but got %q", i, test.marker, marker)
		}

		if comment != test.comment {
			t.Errorf("#%d: expected comment %q, but got %q", i, test.comment, comment)
		}

		if !reflect.DeepEqual(test.hosts, hosts) {
			t.Errorf("#%d: expected hosts %#v, but got %#v", i, test.hosts, hosts)
		}

		if rest := string(rest); rest != test.rest {
			t.Errorf("#%d: expected remaining input to be %q, but got %q", i, test.rest, rest)
		}
	}
}

func TestFingerprintLegacyMD5(t *testing.T) {
	pub, _ := getTestKey()
	fingerprint := FingerprintLegacyMD5(pub)
	want := "b7:ef:d3:d5:89:29:52:96:9f:df:47:41:4d:15:37:f4" // ssh-keygen -lf -E md5 rsa
	if fingerprint != want {
		t.Errorf("got fingerprint %q want %q", fingerprint, want)
	}
}

func TestFingerprintSHA256(t *testing.T) {
	pub, _ := getTestKey()
	fingerprint := FingerprintSHA256(pub)
	want := "SHA256:fi5+D7UmDZDE9Q2sAVvvlpcQSIakN4DERdINgXd2AnE" // ssh-keygen -lf rsa
	if fingerprint != want {
		t.Errorf("got fingerprint %q want %q", fingerprint, want)
	}
}

func TestInvalidKeys(t *testing.T) {
	keyTypes := []string{
		"RSA PRIVATE KEY",
		"PRIVATE KEY",
		"EC PRIVATE KEY",
		"DSA PRIVATE KEY",
		"OPENSSH PRIVATE KEY",
	}

	for _, keyType := range keyTypes {
		for _, dataLen := range []int{0, 1, 2, 5, 10, 20} {
			data := make([]byte, dataLen)
			if _, err := io.ReadFull(rand.Reader, data); err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			pem.Encode(&buf, &pem.Block{
				Type:  keyType,
				Bytes: data,
			})

			// This test is just to ensure that the function
			// doesn't panic so the return value is ignored.
			ParseRawPrivateKey(buf.Bytes())
		}
	}
}

func TestSKKeys(t *testing.T) {
	for _, d := range testdata.SKData {
		pk, _, _, _, err := ParseAuthorizedKey(d.PubKey)
		if err != nil {
			t.Fatalf("parseAuthorizedKey returned error: %v", err)
		}

		sigBuf := make([]byte, hex.DecodedLen(len(d.HexSignature)))
		if _, err := hex.Decode(sigBuf, d.HexSignature); err != nil {
			t.Fatalf("hex.Decode() failed: %v", err)
		}

		dataBuf := make([]byte, hex.DecodedLen(len(d.HexData)))
		if _, err := hex.Decode(dataBuf, d.HexData); err != nil {
			t.Fatalf("hex.Decode() failed: %v", err)
		}

		sig, _, ok := parseSignature(sigBuf)
		if !ok {
			t.Fatalf("parseSignature(%v) failed", sigBuf)
		}

		// Test that good data and signature pass verification
		if err := pk.Verify(dataBuf, sig); err != nil {
			t.Errorf("%s: PublicKey.Verify(%v, %v) failed: %v", d.Name, dataBuf, sig, err)
		}

		// Invalid data being passed in
		invalidData := []byte("INVALID DATA")
		if err := pk.Verify(invalidData, sig); err == nil {
			t.Errorf("%s with invalid data: PublicKey.Verify(%v, %v) passed unexpectedly", d.Name, invalidData, sig)
		}

		// Change byte in blob to corrup signature
		sig.Blob[5] = byte('A')
		// Corrupted data being passed in
		if err := pk.Verify(dataBuf, sig); err == nil {
			t.Errorf("%s with corrupted signature: PublicKey.Verify(%v, %v) passed unexpectedly", d.Name, dataBuf, sig)
		}
	}
}

// skTestHarness builds SK-formatted signatures over a fixed payload
// using a caller-supplied signing function, letting tests vary the UP
// flag byte without duplicating the wire-format scaffolding.
type skTestHarness struct {
	format      string
	application string
	data        []byte
	// sign takes the SHA-256 digest of the marshalled SK blob and
	// returns the value to embed in Signature.Blob. For ECDSA keys the
	// helper feeds the digest to ecdsa.Sign; for ed25519 the helper
	// feeds the raw marshalled blob to ed25519.Sign.
	signDigest func(digest []byte) []byte
	signBlob   func(blob []byte) []byte
}

func (h skTestHarness) sign(t *testing.T, flags byte) *Signature {
	t.Helper()
	hsh := sha256.New()
	hsh.Write([]byte(h.application))
	appDigest := hsh.Sum(nil)

	hsh.Reset()
	hsh.Write(h.data)
	dataDigest := hsh.Sum(nil)

	var counter uint32 = 1
	blob := struct {
		ApplicationDigest []byte `ssh:"rest"`
		Flags             byte
		Counter           uint32
		MessageDigest     []byte `ssh:"rest"`
	}{appDigest, flags, counter, dataDigest}
	marshalled := Marshal(blob)

	var sigBlob []byte
	if h.signDigest != nil {
		hsh.Reset()
		hsh.Write(marshalled)
		sigBlob = h.signDigest(hsh.Sum(nil))
	} else {
		sigBlob = h.signBlob(marshalled)
	}

	return &Signature{
		Format: h.format,
		Blob:   sigBlob,
		Rest: Marshal(struct {
			Flags   byte
			Counter uint32
		}{flags, counter}),
	}
}

func TestSKUserPresence(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	h := skTestHarness{
		format:      "sk-ecdsa-sha2-nistp256@openssh.com",
		application: "ssh:",
		data:        []byte("test data"),
		signDigest: func(digest []byte) []byte {
			r, s, err := ecdsa.Sign(rand.Reader, ecKey, digest)
			if err != nil {
				t.Fatal(err)
			}
			return Marshal(struct{ R, S *big.Int }{r, s})
		},
	}

	pk := &skECDSAPublicKey{
		application: h.application,
		PublicKey:   ecKey.PublicKey,
	}

	// Valid signature with UP=1 should pass.
	if err := pk.Verify(h.data, h.sign(t, flagUserPresence)); err != nil {
		t.Errorf("Verify failed with UP=1: %v", err)
	}

	// Valid signature with UP=0 should fail with the user-presence sentinel.
	sigNoUP := h.sign(t, 0)
	if err := pk.Verify(h.data, sigNoUP); !errors.Is(err, errSKMissingUserPresence) {
		t.Errorf("expected errSKMissingUserPresence, got: %v", err)
	}

	// UV set but UP clear must still fail: we only waive UP, never UV-only.
	if err := pk.Verify(h.data, h.sign(t, 0x04)); !errors.Is(err, errSKMissingUserPresence) {
		t.Errorf("UV-only (flags=0x04): expected errSKMissingUserPresence, got: %v", err)
	}

	// With noTouchRequired, UP=0 passes; UP=1+UV=1 also passes.
	pk.noTouchRequired = true
	if err := pk.Verify(h.data, sigNoUP); err != nil {
		t.Errorf("Verify with noTouchRequired failed: %v", err)
	}
	if err := pk.Verify(h.data, h.sign(t, flagUserPresence|0x04)); err != nil {
		t.Errorf("Verify UP|UV with noTouchRequired failed: %v", err)
	}
}

func TestSKKeyWithoutUP(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	skEC := &skECDSAPublicKey{PublicKey: ecKey.PublicKey}
	skED := &skEd25519PublicKey{PublicKey: edPub}
	certEC := &Certificate{Key: &skECDSAPublicKey{PublicKey: ecKey.PublicKey}}
	certED := &Certificate{Key: &skEd25519PublicKey{PublicKey: edPub}}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, err := NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	certRSA := &Certificate{Key: rsaPub}

	// SK raw keys: return a clone with the flag set, original untouched.
	gotEC := skKeyWithoutUP(skEC)
	if gotEC == skEC {
		t.Error("skECDSAPublicKey: expected a clone, got the original pointer")
	}
	if !gotEC.(*skECDSAPublicKey).noTouchRequired {
		t.Error("skECDSAPublicKey clone: noTouchRequired not set")
	}
	if skEC.noTouchRequired {
		t.Error("skECDSAPublicKey: original mutated")
	}

	gotED := skKeyWithoutUP(skED)
	if gotED == skED {
		t.Error("skEd25519PublicKey: expected a clone, got the original pointer")
	}
	if !gotED.(*skEd25519PublicKey).noTouchRequired {
		t.Error("skEd25519PublicKey clone: noTouchRequired not set")
	}
	if skED.noTouchRequired {
		t.Error("skEd25519PublicKey: original mutated")
	}

	// Certificate wrapping SK: return a clone of the cert with a cloned
	// SK key inside. Neither the original cert nor the original inner
	// key must be mutated.
	originalInnerEC := certEC.Key
	gotCertEC := skKeyWithoutUP(certEC)
	if gotCertEC == certEC {
		t.Error("*Certificate(SK ecdsa): expected a clone, got the original pointer")
	}
	if got := gotCertEC.(*Certificate).Key.(*skECDSAPublicKey); !got.noTouchRequired {
		t.Error("*Certificate(SK ecdsa): inner clone missing noTouchRequired")
	}
	if certEC.Key != originalInnerEC {
		t.Error("*Certificate(SK ecdsa): original cert's Key pointer mutated")
	}
	if originalInnerEC.(*skECDSAPublicKey).noTouchRequired {
		t.Error("*Certificate(SK ecdsa): original inner key mutated")
	}

	gotCertED := skKeyWithoutUP(certED)
	if gotCertED == certED {
		t.Error("*Certificate(SK ed25519): expected a clone, got the original pointer")
	}
	if got := gotCertED.(*Certificate).Key.(*skEd25519PublicKey); !got.noTouchRequired {
		t.Error("*Certificate(SK ed25519): inner clone missing noTouchRequired")
	}

	// Non-SK key inside a cert: return original unchanged (nothing to clone).
	if got := skKeyWithoutUP(certRSA); got != certRSA {
		t.Error("*Certificate(RSA): expected the original pointer back")
	}

	// Plain non-SK key: return original unchanged.
	if got := skKeyWithoutUP(rsaPub); got != rsaPub {
		t.Error("rsaPublicKey: expected the original pointer back")
	}

	// Pathological: *Certificate whose Key is itself a *Certificate.
	// The SSH cert format forbids this and parseCert rejects it, but
	// a Go caller can still construct such a value. skKeyWithoutUP
	// must not recurse into it (or panic); it returns the input
	// unchanged. This also defends against a hypothetical cycle built
	// from hand-constructed Certificate pointers.
	nestedCert := &Certificate{Key: &Certificate{Key: skEC}}
	if got := skKeyWithoutUP(nestedCert); got != nestedCert {
		t.Error("*Certificate wrapping *Certificate: expected the original pointer back")
	}
	selfCycle := &Certificate{}
	selfCycle.Key = selfCycle
	if got := skKeyWithoutUP(selfCycle); got != selfCycle {
		t.Error("self-referential *Certificate: expected the original pointer back")
	}
}

func TestNoTouchAllowed(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sk := &skECDSAPublicKey{PublicKey: ecKey.PublicKey}
	certNoExt := &Certificate{Key: sk}
	certOptOut := &Certificate{
		Key:         sk,
		Permissions: Permissions{Extensions: map[string]string{"no-touch-required": ""}},
	}
	// Non-empty value: OpenSSH writes "" but any value must be accepted
	// because the check is presence-only.
	certOptOutNonEmpty := &Certificate{
		Key:         sk,
		Permissions: Permissions{Extensions: map[string]string{"no-touch-required": "yes"}},
	}
	// no-touch-required belongs in Extensions, never CriticalOptions.
	// Putting it in CriticalOptions must NOT be treated as opt-out.
	certCritOnly := &Certificate{
		Key:         sk,
		Permissions: Permissions{CriticalOptions: map[string]string{"no-touch-required": ""}},
	}

	permsEmpty := &Permissions{}
	permsOptOut := &Permissions{Extensions: map[string]string{"no-touch-required": ""}}
	permsCritOnly := &Permissions{CriticalOptions: map[string]string{"no-touch-required": ""}}

	cases := []struct {
		name  string
		pub   PublicKey
		perms *Permissions
		want  bool
	}{
		{"nil perms, no cert", sk, nil, false},
		{"empty perms, no cert", sk, permsEmpty, false},
		{"perms opt-out, raw key", sk, permsOptOut, true},
		{"nil perms, cert opt-out", certOptOut, nil, true},
		{"nil perms, cert opt-out non-empty value", certOptOutNonEmpty, nil, true},
		{"nil perms, cert no ext", certNoExt, nil, false},
		{"perms opt-out, cert no ext", certNoExt, permsOptOut, true},
		{"empty perms, cert opt-out", certOptOut, permsEmpty, true},
		// Negative controls: CriticalOptions must not waive UP.
		{"critical-options only, raw key", sk, permsCritOnly, false},
		{"critical-options only, cert", certCritOnly, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := noTouchAllowed(tc.pub, tc.perms); got != tc.want {
				t.Errorf("noTouchAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSKUserPresenceEd25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := skTestHarness{
		format:      "sk-ssh-ed25519@openssh.com",
		application: "ssh:",
		data:        []byte("test data"),
		signBlob: func(blob []byte) []byte {
			return ed25519.Sign(priv, blob)
		},
	}

	pk := &skEd25519PublicKey{
		application: h.application,
		PublicKey:   pub,
	}

	if err := pk.Verify(h.data, h.sign(t, flagUserPresence)); err != nil {
		t.Errorf("Verify failed with UP=1: %v", err)
	}
	sigNoUP := h.sign(t, 0)
	if err := pk.Verify(h.data, sigNoUP); !errors.Is(err, errSKMissingUserPresence) {
		t.Errorf("expected errSKMissingUserPresence, got: %v", err)
	}
	pk.noTouchRequired = true
	if err := pk.Verify(h.data, sigNoUP); err != nil {
		t.Errorf("Verify with noTouchRequired failed: %v", err)
	}
}

func TestNewSignerWithAlgos(t *testing.T) {
	algorithSigner, ok := testSigners["rsa"].(AlgorithmSigner)
	if !ok {
		t.Fatal("rsa test signer does not implement the AlgorithmSigner interface")
	}
	_, err := NewSignerWithAlgorithms(algorithSigner, nil)
	if err == nil {
		t.Error("signer with algos created with no algorithms")
	}

	_, err = NewSignerWithAlgorithms(algorithSigner, []string{KeyAlgoED25519})
	if err == nil {
		t.Error("signer with algos created with invalid algorithms")
	}

	_, err = NewSignerWithAlgorithms(algorithSigner, []string{CertAlgoRSASHA256v01})
	if err == nil {
		t.Error("signer with algos created with certificate algorithms")
	}

	mas, err := NewSignerWithAlgorithms(algorithSigner, []string{KeyAlgoRSASHA256, KeyAlgoRSASHA512})
	if err != nil {
		t.Errorf("unable to create signer with valid algorithms: %v", err)
	}

	_, err = NewSignerWithAlgorithms(mas, []string{KeyAlgoRSA})
	if err == nil {
		t.Error("signer with algos created with restricted algorithms")
	}
}

func TestCryptoPublicKey(t *testing.T) {
	for _, priv := range testSigners {
		p1 := priv.PublicKey()
		key, ok := p1.(CryptoPublicKey)
		if !ok {
			continue
		}
		p2, err := NewPublicKey(key.CryptoPublicKey())
		if err != nil {
			t.Fatalf("NewPublicKey(CryptoPublicKey) failed for %s, got: %v", p1.Type(), err)
		}
		if !reflect.DeepEqual(p1, p2) {
			t.Errorf("got %#v in NewPublicKey, want %#v", p2, p1)
		}
	}
	for _, d := range testdata.SKData {
		p1, _, _, _, err := ParseAuthorizedKey(d.PubKey)
		if err != nil {
			t.Fatalf("parseAuthorizedKey returned error: %v", err)
		}
		k1, ok := p1.(CryptoPublicKey)
		if !ok {
			t.Fatalf("%T does not implement CryptoPublicKey", p1)
		}

		var p2 PublicKey
		switch pub := k1.CryptoPublicKey().(type) {
		case *ecdsa.PublicKey:
			p2 = &skECDSAPublicKey{
				application: "ssh:",
				PublicKey:   *pub,
			}
		case ed25519.PublicKey:
			p2 = &skEd25519PublicKey{
				application: "ssh:",
				PublicKey:   pub,
			}
		default:
			t.Fatalf("unexpected type %T from CryptoPublicKey()", pub)
		}
		if !reflect.DeepEqual(p1, p2) {
			t.Errorf("got %#v, want %#v", p2, p1)
		}
	}
}

func TestParseCertWithCertSignatureKey(t *testing.T) {
	certBytes := []byte(`-----BEGIN SSH CERTIFICATE-----
AAAAIHNzaC1lZDI1NTE5LWNlcnQtdjAxQG9wZW5zc2guY29tAAAAIPSp27hvNSB0
IotJnVhjC4zxNgNS8BHlUCxD0VJi4D/eAAAAIIJMi1e5qfx+IFuKD/p/Ssqcb3os
CpOw/4wBs1pQ53zwAAAAAAAAAAEAAAACAAAAAAAAABMAAAAPZm9vLmV4YW1wbGUu
Y29tAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAT0AAAAgc3NoLWVkMjU1
MTktY2VydC12MDFAb3BlbnNzaC5jb20AAAAg+sNYhCO35mQT1UBMpmMk8ey+culd
IU8vBlPEl4B07swAAAAggiv+RLnboS4znGCVl/n1jDg2uD0h15tW4s/04eS2mLQA
AAAAAAAAAQAAAAIAAAAAAAAAEwAAAA9mb28uZXhhbXBsZS5jb20AAAAAAAAAAAAA
AAAAAAAAAAAAAAAAAAAAAAAAAAAAMwAAAAtzc2gtZWQyNTUxOQAAACCV2wETgLKL
Kt0bRl3YUnd/ZYSlq0xJMbn4Jj3cdPWykQAAAFMAAAALc3NoLWVkMjU1MTkAAABA
WOdbRGEzyRAhiIK227CLUQD5caXYMV8FvSIB7toEE2M/8HnWdG9H3Rsg/v3unruQ
JrQldnuPJNe7KOP2+zvUDgAAAFMAAAALc3NoLWVkMjU1MTkAAABAm3bIPp85ZpIe
D+izJcUqlcAOri7HO8bULFNHT6LVegvB06xQ5TLwMlrxWUF4cafl1tSe8JQck4a6
cLYUOHfQDw==
-----END SSH CERTIFICATE-----
	`)
	block, _ := pem.Decode(certBytes)
	if block == nil {
		t.Fatal("invalid test certificate")
	}

	if _, err := ParsePublicKey(block.Bytes); err == nil {
		t.Fatal("parsing an SSH certificate using another certificate as signature key succeeded; expected failure")
	}
}

func TestParseECDSAAlgorithmMismatch(t *testing.T) {
	cases := []struct {
		keyName    string // key fixture in testPublicKeys
		nativeAlgo string // algorithm actually carried in the key blob
		askedAlgo  string // algorithm passed to parsePubKey
	}{
		{"ecdsap256", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384"},
		{"ecdsap256", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp521"},
		{"ecdsap384", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp256"},
		{"ecdsap384", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"},
		{"ecdsap521", "ecdsa-sha2-nistp521", "ecdsa-sha2-nistp256"},
		{"ecdsap521", "ecdsa-sha2-nistp521", "ecdsa-sha2-nistp384"},
	}

	for _, tc := range cases {
		t.Run(tc.nativeAlgo+"_as_"+tc.askedAlgo, func(t *testing.T) {
			pubKey := testPublicKeys[tc.keyName]
			algo, in, ok := parseString(pubKey.Marshal())
			if !ok {
				t.Fatal("unable to parse public key wire format")
			}
			if string(algo) != tc.nativeAlgo {
				t.Fatalf("test setup failed: expected %q, got %q", tc.nativeAlgo, algo)
			}

			_, _, err := parsePubKey(in, tc.askedAlgo)
			if err == nil {
				t.Fatal("expected error due to algorithm mismatch, but got nil")
			}
			if !strings.Contains(err.Error(), "algorithm type mismatch") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}
