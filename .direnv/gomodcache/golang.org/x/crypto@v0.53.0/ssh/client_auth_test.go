// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type keyboardInteractive map[string]string

func (cr keyboardInteractive) Challenge(user string, instruction string, questions []string, echos []bool) ([]string, error) {
	var answers []string
	for _, q := range questions {
		answers = append(answers, cr[q])
	}
	return answers, nil
}

// reused internally by tests
var clientPassword = "tiger"

// tryAuth runs a handshake with a given config against an SSH server
// with config serverConfig. Returns both client and server side errors.
func tryAuth(t *testing.T, config *ClientConfig) error {
	err, _ := tryAuthBothSides(t, config, nil)
	return err
}

// tryAuthWithGSSAPIWithMICConfig runs a handshake with a given config against an SSH server
// with a given GSSAPIWithMICConfig and config serverConfig. Returns both client and server side errors.
func tryAuthWithGSSAPIWithMICConfig(t *testing.T, clientConfig *ClientConfig, gssAPIWithMICConfig *GSSAPIWithMICConfig) error {
	err, _ := tryAuthBothSides(t, clientConfig, gssAPIWithMICConfig)
	return err
}

// tryAuthBothSides runs the handshake and returns the resulting errors from both sides of the connection.
func tryAuthBothSides(t *testing.T, config *ClientConfig, gssAPIWithMICConfig *GSSAPIWithMICConfig) (clientError error, serverAuthErrors []error) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	certChecker := CertChecker{
		IsUserAuthority: func(k PublicKey) bool {
			return bytes.Equal(k.Marshal(), testPublicKeys["ecdsa"].Marshal())
		},
		UserKeyFallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if conn.User() == "testuser" && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}

			return nil, fmt.Errorf("pubkey for %q not acceptable", conn.User())
		},
		IsRevoked: func(c *Certificate) bool {
			return c.Serial == 666
		},
	}
	serverConfig := &ServerConfig{
		PasswordCallback: func(conn ConnMetadata, pass []byte) (*Permissions, error) {
			if conn.User() == "testuser" && string(pass) == clientPassword {
				return nil, nil
			}
			return nil, errors.New("password auth failed")
		},
		PublicKeyCallback: certChecker.Authenticate,
		KeyboardInteractiveCallback: func(conn ConnMetadata, challenge KeyboardInteractiveChallenge) (*Permissions, error) {
			ans, err := challenge("user",
				"instruction",
				[]string{"question1", "question2"},
				[]bool{true, true})
			if err != nil {
				return nil, err
			}
			ok := conn.User() == "testuser" && ans[0] == "answer1" && ans[1] == "answer2"
			if ok {
				challenge("user", "motd", nil, nil)
				return nil, nil
			}
			return nil, errors.New("keyboard-interactive failed")
		},
		GSSAPIWithMICConfig: gssAPIWithMICConfig,
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	serverConfig.AuthLogCallback = func(conn ConnMetadata, method string, err error) {
		serverAuthErrors = append(serverAuthErrors, err)
	}

	go newServer(c1, serverConfig)
	_, _, _, err = NewClientConn(c2, "", config)
	return err, serverAuthErrors
}

type loggingAlgorithmSigner struct {
	used []string
	AlgorithmSigner
}

func (l *loggingAlgorithmSigner) Sign(rand io.Reader, data []byte) (*Signature, error) {
	l.used = append(l.used, "[Sign]")
	return l.AlgorithmSigner.Sign(rand, data)
}

func (l *loggingAlgorithmSigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*Signature, error) {
	l.used = append(l.used, algorithm)
	return l.AlgorithmSigner.SignWithAlgorithm(rand, data, algorithm)
}

func TestClientAuthPublicKey(t *testing.T) {
	signer := &loggingAlgorithmSigner{AlgorithmSigner: testSigners["rsa"].(AlgorithmSigner)}
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(signer),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
	if len(signer.used) != 1 || signer.used[0] != KeyAlgoRSASHA256 {
		t.Errorf("unexpected Sign/SignWithAlgorithm calls: %q", signer.used)
	}
}

// TestClientAuthNoSHA2 tests a ssh-rsa Signer that doesn't implement AlgorithmSigner.
func TestClientAuthNoSHA2(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(&legacyRSASigner{testSigners["rsa"]}),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

// TestClientAuthThirdKey checks that the third configured can succeed. If we
// were to do three attempts for each key (rsa-sha2-256, rsa-sha2-512, ssh-rsa),
// we'd hit the six maximum attempts before reaching it.
func TestClientAuthThirdKey(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa-openssh-format"],
				testSigners["rsa-openssh-format"], testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

// partialSuccessPublicKeyAndKbdInteractiveServer returns a server config
// where the rsa key gets partial success and the server offers both publickey
// and keyboard-interactive as next methods. The VerifiedPublicKeyCallback
// tracks state so that the same key only triggers partial success once,
// avoiding an impossible auth loop.
func partialSuccessPublicKeyAndKbdInteractiveServer() *ServerConfig {
	rsaDone := false
	serverConf := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
		VerifiedPublicKeyCallback: func(conn ConnMetadata, key PublicKey, permissions *Permissions, signatureAlgorithm string) (*Permissions, error) {
			if !rsaDone && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				rsaDone = true
				return nil, &PartialSuccessError{
					Next: ServerAuthCallbacks{
						PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
							if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
								return nil, nil
							}
							return nil, errors.New("invalid credentials")
						},
						KeyboardInteractiveCallback: func(conn ConnMetadata, client KeyboardInteractiveChallenge) (*Permissions, error) {
							answers, err := client("", "", []string{"token: "}, []bool{true})
							if err != nil {
								return nil, err
							}
							if len(answers) == 1 && answers[0] == "correct-token" {
								return nil, nil
							}
							return nil, errors.New("invalid token")
						},
					},
				}
			}
			return nil, errors.New("invalid credentials")
		},
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

// partialSuccessMultiKeyServer returns a server config that requires two
// public keys in sequence: the first key (rsa) triggers partial success
// requiring a second key (ecdsa) to complete authentication.
func partialSuccessMultiKeyServer() *ServerConfig {
	firstKeyDone := false
	serverConf := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}
			if bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
		VerifiedPublicKeyCallback: func(conn ConnMetadata, key PublicKey, permissions *Permissions, signatureAlgorithm string) (*Permissions, error) {
			if !firstKeyDone && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				firstKeyDone = true
				return nil, &PartialSuccessError{
					Next: ServerAuthCallbacks{
						PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
							if bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
								return nil, nil
							}
							return nil, errors.New("invalid credentials")
						},
					},
				}
			}
			if firstKeyDone && bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

func TestStaticMultiKeyPartialSuccess(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"], testSigners["ecdsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	go NewServerConn(c1, partialSuccessMultiKeyServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestStaticPartialSuccess(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"]),
			KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				return []string{"correct-token"}, nil
			}),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	go NewServerConn(c1, partialSuccessPublicKeyAndKbdInteractiveServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

// partialSuccessKeyAThenKeyBServer returns a server config where keyA (rsa)
// gets partial success and the server offers BOTH publickey and
// keyboard-interactive. keyB (ecdsa) then completes auth successfully.
func partialSuccessKeyAThenKeyBServer() *ServerConfig {
	keyADone := false
	serverConf := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}
			if bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
		VerifiedPublicKeyCallback: func(conn ConnMetadata, key PublicKey, permissions *Permissions, signatureAlgorithm string) (*Permissions, error) {
			if !keyADone && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				keyADone = true
				return nil, &PartialSuccessError{
					Next: ServerAuthCallbacks{
						PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
							if bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
								return nil, nil
							}
							return nil, errors.New("invalid credentials")
						},
						KeyboardInteractiveCallback: func(conn ConnMetadata, client KeyboardInteractiveChallenge) (*Permissions, error) {
							answers, err := client("", "", []string{"token: "}, []bool{true})
							if err != nil {
								return nil, err
							}
							if len(answers) == 1 && answers[0] == "correct-token" {
								return nil, nil
							}
							return nil, errors.New("invalid token")
						},
					},
				}
			}
			if keyADone && bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

func TestStaticPartialSuccessKeyAThenKeyB(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"], testSigners["ecdsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	go NewServerConn(c1, partialSuccessKeyAThenKeyBServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

// partialSuccessMultiKeyThenKbdInteractiveServer returns a server config
// that strictly requires keyA (rsa) AND keyB (ecdsa) as public keys before
// allowing keyboard-interactive. After keyA, ONLY publickey is offered
// (for keyB). After keyB, ONLY keyboard-interactive is offered.
func partialSuccessMultiKeyThenKbdInteractiveServer() *ServerConfig {
	serverConf := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, &PartialSuccessError{
					Next: ServerAuthCallbacks{
						PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
							if bytes.Equal(key.Marshal(), testPublicKeys["ecdsa"].Marshal()) {
								return nil, &PartialSuccessError{
									Next: ServerAuthCallbacks{
										KeyboardInteractiveCallback: func(conn ConnMetadata, client KeyboardInteractiveChallenge) (*Permissions, error) {
											answers, err := client("", "", []string{"token: "}, []bool{true})
											if err != nil {
												return nil, err
											}
											if len(answers) == 1 && answers[0] == "correct-token" {
												return nil, nil
											}
											return nil, errors.New("invalid token")
										},
									},
								}
							}
							return nil, errors.New("invalid credentials")
						},
					},
				}
			}
			return nil, errors.New("invalid credentials")
		},
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

func TestStaticMultiKeyPartialThenKbdInteractive(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"], testSigners["ecdsa"]),
			KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				return []string{"correct-token"}, nil
			}),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	go NewServerConn(c1, partialSuccessMultiKeyThenKbdInteractiveServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthMethodDynamic(t *testing.T) {
	config := &ClientConfig{
		User:            "testuser",
		Auth:            nil,
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if len(ctx.Metadata.SessionID()) == 0 {
				t.Error("empty sessionID")
			}
			if ctx.Algorithms.HostKey == "" {
				t.Error("empty Host Key")
			}
			if slices.Contains(ctx.AllowedMethods, "password") {
				return Password(clientPassword), nil
			}
			return nil, errors.New("auth callback error")
		},
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestAuthCallbackError(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			Password(clientPassword),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			return nil, errors.New("auth callback error")
		},
	}

	if err := tryAuth(t, config); err == nil {
		t.Fatal("authentication unexpectedly succeeded after AuthCallback returned an error")
	}
}

func TestAuthCallbackPartialSuccess(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if slices.Contains(ctx.AllowedMethods, "publickey") && !slices.Contains(ctx.PartialSuccessMethods, "publickey") {
				return PublicKeys(testSigners["rsa"]), nil
			}
			if slices.Contains(ctx.PartialSuccessMethods, "publickey") && slices.Contains(ctx.AllowedMethods, "keyboard-interactive") {
				return KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					return []string{"correct-token"}, nil
				}), nil
			}
			return nil, fmt.Errorf("unexpected auth state: allowed=%v partial=%v", ctx.AllowedMethods, ctx.PartialSuccessMethods)
		},
	}

	go NewServerConn(c1, partialSuccessPublicKeyAndKbdInteractiveServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthCallbackMultiKeyPartialSuccess(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if slices.Contains(ctx.AllowedMethods, "publickey") && !slices.Contains(ctx.PartialSuccessMethods, "publickey") {
				return PublicKeys(testSigners["rsa"]), nil
			}
			if slices.Contains(ctx.PartialSuccessMethods, "publickey") && slices.Contains(ctx.AllowedMethods, "publickey") {
				return PublicKeys(testSigners["ecdsa"]), nil
			}
			return nil, fmt.Errorf("unexpected auth state: allowed=%v partial=%v", ctx.AllowedMethods, ctx.PartialSuccessMethods)
		},
	}

	go NewServerConn(c1, partialSuccessMultiKeyServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthCallbackMultiKeyPartialThenKbdInteractive(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			partialCount := len(ctx.PartialSuccessMethods)
			if partialCount == 0 && slices.Contains(ctx.AllowedMethods, "publickey") {
				return PublicKeys(testSigners["rsa"]), nil
			}
			if partialCount == 1 && slices.Contains(ctx.AllowedMethods, "publickey") {
				return PublicKeys(testSigners["ecdsa"]), nil
			}
			if partialCount >= 2 && slices.Contains(ctx.AllowedMethods, "keyboard-interactive") {
				return KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					return []string{"correct-token"}, nil
				}), nil
			}
			return nil, fmt.Errorf("unexpected auth state: allowed=%v partial=%v tried=%v",
				ctx.AllowedMethods, ctx.PartialSuccessMethods, ctx.TriedMethods)
		},
	}

	go NewServerConn(c1, partialSuccessMultiKeyThenKbdInteractiveServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthCallbackPartialSuccessKeyAThenKeyB(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if len(ctx.PartialSuccessMethods) == 0 && slices.Contains(ctx.AllowedMethods, "publickey") {
				return PublicKeys(testSigners["rsa"]), nil
			}
			if len(ctx.PartialSuccessMethods) == 1 && slices.Contains(ctx.AllowedMethods, "publickey") {
				return PublicKeys(testSigners["ecdsa"]), nil
			}
			return nil, fmt.Errorf("unexpected auth state: allowed=%v partial=%v",
				ctx.AllowedMethods, ctx.PartialSuccessMethods)
		},
	}

	go NewServerConn(c1, partialSuccessKeyAThenKeyBServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthCallbackPartialSuccessWithStaticAuth(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if slices.Contains(ctx.PartialSuccessMethods, "publickey") && slices.Contains(ctx.AllowedMethods, "keyboard-interactive") {
				return KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					return []string{"correct-token"}, nil
				}), nil
			}
			return nil, nil
		},
	}

	go NewServerConn(c1, partialSuccessPublicKeyAndKbdInteractiveServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func partialSuccessPubKeyForbiddenAfterPartialServer(maxTries int) *ServerConfig {
	serverConf := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
		VerifiedPublicKeyCallback: func(conn ConnMetadata, key PublicKey, permissions *Permissions, signatureAlgorithm string) (*Permissions, error) {
			if bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, &PartialSuccessError{
					Next: ServerAuthCallbacks{
						PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
							return nil, errors.New("legacy public key authentication is forbidden")
						},
						KeyboardInteractiveCallback: func(conn ConnMetadata, client KeyboardInteractiveChallenge) (*Permissions, error) {
							answers, err := client("", "", []string{"token: "}, []bool{true})
							if err != nil {
								return nil, err
							}
							if len(answers) == 1 && answers[0] == "correct-token" {
								return nil, nil
							}
							return nil, errors.New("invalid token")
						},
					},
				}
			}
			return nil, errors.New("invalid credentials")
		},
		MaxAuthTries: maxTries,
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

func TestAuthCallbackPartialSuccessWithStaticMultiKey(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	clientConf := &ClientConfig{
		User: "user",
		Auth: []AuthMethod{
			PublicKeys(
				testSigners["rsa"],
				testSigners["ecdsa"],
				testSigners["ed25519"],
			),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if slices.Contains(ctx.PartialSuccessMethods, "publickey") && slices.Contains(ctx.AllowedMethods, "keyboard-interactive") {
				return KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					return []string{"correct-token"}, nil
				}), nil
			}
			return nil, nil
		},
	}

	go NewServerConn(c1, partialSuccessPubKeyForbiddenAfterPartialServer(2))

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
}

func TestAuthCallbackPartialSuccessShortCircuit(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	serverConf := partialSuccessPubKeyForbiddenAfterPartialServer(2)

	pubkeyAttempts := 0
	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			if slices.Contains(ctx.AllowedMethods, "publickey") && !slices.Contains(ctx.PartialSuccessMethods, "publickey") {
				pubkeyAttempts++
				return PublicKeys(testSigners["rsa"], testSigners["ecdsa"], testSigners["ed25519"]), nil
			}
			if slices.Contains(ctx.PartialSuccessMethods, "publickey") && slices.Contains(ctx.AllowedMethods, "keyboard-interactive") {
				return KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					return []string{"correct-token"}, nil
				}), nil
			}
			return nil, fmt.Errorf("unexpected auth state: allowed=%v partial=%v tried=%v",
				ctx.AllowedMethods, ctx.PartialSuccessMethods, ctx.TriedMethods)
		},
	}

	go NewServerConn(c1, serverConf)

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err != nil {
		t.Fatalf("client login error: %s", err)
	}
	if pubkeyAttempts != 1 {
		t.Errorf("expected publickey to be attempted once before short-circuit, got %d", pubkeyAttempts)
	}
}

func TestAuthCallbackTriedLoopBound(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	serverConf := &ServerConfig{
		MaxAuthTries: -1,
		PasswordCallback: func(conn ConnMetadata, pass []byte) (*Permissions, error) {
			return nil, errors.New("password rejected")
		},
	}
	serverConf.AddHostKey(testSigners["rsa"])

	invocations := 0
	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			invocations++
			return Password("wrong"), nil
		},
	}

	go NewServerConn(c1, serverConf)

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err == nil {
		t.Fatal("expected the client to abort after exceeding the attempts cap")
	}
	if !strings.Contains(err.Error(), "too many authentication attempts") {
		t.Errorf("unexpected error: %v", err)
	}
	if invocations != maxAuthClientTried {
		t.Errorf("AuthCallback invoked %d times; expected %d",
			invocations, maxAuthClientTried)
	}
}

func alwaysPartialPubKeyServer() *ServerConfig {
	var alwaysPartial func(ConnMetadata, PublicKey) (*Permissions, error)
	alwaysPartial = func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
		if !bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
			return nil, errors.New("invalid credentials")
		}
		return nil, &PartialSuccessError{
			Next: ServerAuthCallbacks{
				PublicKeyCallback: alwaysPartial,
			},
		}
	}
	serverConf := &ServerConfig{
		MaxAuthTries:      -1,
		PublicKeyCallback: alwaysPartial,
	}
	serverConf.AddHostKey(testSigners["rsa"])
	return serverConf
}

func TestAuthCallbackPartialSuccessLoopBound(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	invocations := 0
	clientConf := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		AuthCallback: func(ctx *ClientAuthContext) (AuthMethod, error) {
			invocations++
			return PublicKeys(testSigners["rsa"]), nil
		},
	}

	go NewServerConn(c1, alwaysPartialPubKeyServer())

	_, _, _, err = NewClientConn(c2, "", clientConf)
	if err == nil {
		t.Fatal("expected the client to abort after exceeding the attempts cap")
	}
	if !strings.Contains(err.Error(), "too many authentication attempts") {
		t.Errorf("unexpected error: %v", err)
	}
	if invocations != maxAuthClientTried {
		t.Errorf("AuthCallback invoked %d times; expected %d",
			invocations, maxAuthClientTried)
	}
}

func TestAuthMethodPassword(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			Password(clientPassword),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestAuthMethodFallback(t *testing.T) {
	var passwordCalled bool
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"]),
			PasswordCallback(
				func() (string, error) {
					passwordCalled = true
					return "WRONG", nil
				}),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}

	if passwordCalled {
		t.Errorf("password auth tried before public-key auth.")
	}
}

func TestAuthMethodWrongPassword(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			Password("wrong"),
			PublicKeys(testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestAuthMethodKeyboardInteractive(t *testing.T) {
	answers := keyboardInteractive(map[string]string{
		"question1": "answer1",
		"question2": "answer2",
	})
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			KeyboardInteractive(answers.Challenge),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestAuthMethodWrongKeyboardInteractive(t *testing.T) {
	answers := keyboardInteractive(map[string]string{
		"question1": "answer1",
		"question2": "WRONG",
	})
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			KeyboardInteractive(answers.Challenge),
		},
	}

	if err := tryAuth(t, config); err == nil {
		t.Fatalf("wrong answers should not have authenticated with KeyboardInteractive")
	}
}

// the mock server will only authenticate ssh-rsa keys
func TestAuthMethodInvalidPublicKey(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(testSigners["dsa"]),
		},
	}

	if err := tryAuth(t, config); err == nil {
		t.Fatalf("dsa private key should not have authenticated with rsa public key")
	}
}

// the client should authenticate with the second key
func TestAuthMethodRSAandDSA(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(testSigners["dsa"], testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, config); err != nil {
		t.Fatalf("client could not authenticate with rsa key: %v", err)
	}
}

type invalidAlgSigner struct {
	Signer
}

func (s *invalidAlgSigner) Sign(rand io.Reader, data []byte) (*Signature, error) {
	sig, err := s.Signer.Sign(rand, data)
	if sig != nil {
		sig.Format = "invalid"
	}
	return sig, err
}

func TestMethodInvalidAlgorithm(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(&invalidAlgSigner{testSigners["rsa"]}),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	err, serverErrors := tryAuthBothSides(t, config, nil)
	if err == nil {
		t.Fatalf("login succeeded")
	}

	found := false
	want := "algorithm \"invalid\""

	var errStrings []string
	for _, err := range serverErrors {
		found = found || (err != nil && strings.Contains(err.Error(), want))
		errStrings = append(errStrings, err.Error())
	}
	if !found {
		t.Errorf("server got error %q, want substring %q", errStrings, want)
	}
}

func TestClientHMAC(t *testing.T) {
	supportedAlgos := SupportedAlgorithms()
	insecureAlgos := InsecureAlgorithms()
	supportedMACs := append(supportedAlgos.MACs, insecureAlgos.MACs...)
	for _, mac := range supportedMACs {
		config := &ClientConfig{
			User: "testuser",
			Auth: []AuthMethod{
				PublicKeys(testSigners["rsa"]),
			},
			Config: Config{
				MACs: []string{mac},
			},
			HostKeyCallback: InsecureIgnoreHostKey(),
		}
		if err := tryAuth(t, config); err != nil {
			t.Fatalf("client could not authenticate with mac algo %s: %v", mac, err)
		}
	}
}

// issue 4285.
func TestClientUnsupportedCipher(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(),
		},
		Config: Config{
			Ciphers: []string{"unsupported-cipher"}, // not currently supported
		},
	}
	if err := tryAuth(t, config); err == nil {
		t.Errorf("expected no ciphers in common")
	}
}

func TestClientUnsupportedKex(t *testing.T) {
	if os.Getenv("GO_BUILDER_NAME") != "" {
		t.Skip("skipping known-flaky test on the Go build dashboard; see golang.org/issue/15198")
	}
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(),
		},
		Config: Config{
			KeyExchanges: []string{"non-existent-kex"},
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, config); err == nil || !strings.Contains(err.Error(), "common algorithm") {
		t.Errorf("got %v, expected 'common algorithm'", err)
	}
}

func TestClientLoginCert(t *testing.T) {
	cert := &Certificate{
		Key:         testPublicKeys["rsa"],
		ValidBefore: CertTimeInfinity,
		CertType:    UserCert,
	}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	certSigner, err := NewCertSigner(cert, testSigners["rsa"])
	if err != nil {
		t.Fatalf("NewCertSigner: %v", err)
	}

	clientConfig := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	clientConfig.Auth = append(clientConfig.Auth, PublicKeys(certSigner))

	// should succeed
	if err := tryAuth(t, clientConfig); err != nil {
		t.Errorf("cert login failed: %v", err)
	}

	// corrupted signature
	cert.Signature.Blob[0]++
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login passed with corrupted sig")
	}

	// revoked
	cert.Serial = 666
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("revoked cert login succeeded")
	}
	cert.Serial = 1

	// sign with wrong key
	cert.SignCert(rand.Reader, testSigners["dsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login passed with non-authoritative key")
	}

	// host cert
	cert.CertType = HostCert
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login passed with wrong type")
	}
	cert.CertType = UserCert

	// principal specified
	cert.ValidPrincipals = []string{"user"}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err != nil {
		t.Errorf("cert login failed: %v", err)
	}

	// wrong principal specified
	cert.ValidPrincipals = []string{"fred"}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login passed with wrong principal")
	}
	cert.ValidPrincipals = nil

	// added critical option
	cert.CriticalOptions = map[string]string{"root-access": "yes"}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login passed with unrecognized critical option")
	}

	// allowed source address
	cert.CriticalOptions = map[string]string{"source-address": "127.0.0.42/24,::42/120"}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err != nil {
		t.Errorf("cert login with source-address failed: %v", err)
	}

	// disallowed source address
	cert.CriticalOptions = map[string]string{"source-address": "127.0.0.42,::42"}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	if err := tryAuth(t, clientConfig); err == nil {
		t.Errorf("cert login with source-address succeeded")
	}
}

func testPermissionsPassing(withPermissions bool, t *testing.T) {
	serverConfig := &ServerConfig{
		PublicKeyCallback: func(conn ConnMetadata, key PublicKey) (*Permissions, error) {
			if conn.User() == "nopermissions" {
				return nil, nil
			}
			return &Permissions{}, nil
		},
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	clientConfig := &ClientConfig{
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if withPermissions {
		clientConfig.User = "permissions"
	} else {
		clientConfig.User = "nopermissions"
	}

	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	go NewClientConn(c2, "", clientConfig)
	serverConn, err := newServer(c1, serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	if p := serverConn.Permissions; (p != nil) != withPermissions {
		t.Fatalf("withPermissions is %t, but Permissions object is %#v", withPermissions, p)
	}
}

func TestPermissionsPassing(t *testing.T) {
	testPermissionsPassing(true, t)
}

func TestNoPermissionsPassing(t *testing.T) {
	testPermissionsPassing(false, t)
}

func TestRetryableAuth(t *testing.T) {
	n := 0
	passwords := []string{"WRONG1", "WRONG2"}

	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			RetryableAuthMethod(PasswordCallback(func() (string, error) {
				p := passwords[n]
				n++
				return p, nil
			}), 2),
			PublicKeys(testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	if err := tryAuth(t, config); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
	if n != 2 {
		t.Fatalf("Did not try all passwords")
	}
}

func ExampleRetryableAuthMethod() {
	user := "testuser"
	NumberOfPrompts := 3

	// Normally this would be a callback that prompts the user to answer the
	// provided questions
	Cb := func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
		return []string{"answer1", "answer2"}, nil
	}

	config := &ClientConfig{
		HostKeyCallback: InsecureIgnoreHostKey(),
		User:            user,
		Auth: []AuthMethod{
			RetryableAuthMethod(KeyboardInteractiveChallenge(Cb), NumberOfPrompts),
		},
	}

	host := "mysshserver"
	netConn, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatal(err)
	}

	sshConn, _, _, err := NewClientConn(netConn, host, config)
	if err != nil {
		log.Fatal(err)
	}
	_ = sshConn
}

// Test if username is received on server side when NoClientAuth is used
func TestClientAuthNone(t *testing.T) {
	user := "testuser"
	serverConfig := &ServerConfig{
		NoClientAuth: true,
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	clientConfig := &ClientConfig{
		User:            user,
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	go NewClientConn(c2, "", clientConfig)
	serverConn, err := newServer(c1, serverConfig)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	if serverConn.User() != user {
		t.Fatalf("server: got %q, want %q", serverConn.User(), user)
	}
}

// Test if authentication attempts are limited on server when MaxAuthTries is set
func TestClientAuthMaxAuthTries(t *testing.T) {
	user := "testuser"

	serverConfig := &ServerConfig{
		MaxAuthTries: 2,
		PasswordCallback: func(conn ConnMetadata, pass []byte) (*Permissions, error) {
			if conn.User() == "testuser" && string(pass) == "right" {
				return nil, nil
			}
			return nil, errors.New("password auth failed")
		},
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	expectedErr := fmt.Errorf("ssh: handshake failed: %v", &disconnectMsg{
		Reason:  2,
		Message: "too many authentication failures",
	})

	for tries := 2; tries < 4; tries++ {
		n := tries
		clientConfig := &ClientConfig{
			User: user,
			Auth: []AuthMethod{
				RetryableAuthMethod(PasswordCallback(func() (string, error) {
					n--
					if n == 0 {
						return "right", nil
					}
					return "wrong", nil
				}), tries),
			},
			HostKeyCallback: InsecureIgnoreHostKey(),
		}

		c1, c2, err := netPipe()
		if err != nil {
			t.Fatalf("netPipe: %v", err)
		}
		defer c1.Close()
		defer c2.Close()

		errCh := make(chan error, 1)

		go func() {
			_, err := newServer(c1, serverConfig)
			errCh <- err
		}()
		_, _, _, cliErr := NewClientConn(c2, "", clientConfig)
		srvErr := <-errCh

		if tries > serverConfig.MaxAuthTries {
			if cliErr == nil {
				t.Fatalf("client: got no error, want %s", expectedErr)
			} else if cliErr.Error() != expectedErr.Error() {
				t.Fatalf("client: got %s, want %s", err, expectedErr)
			}
			var authErr *ServerAuthError
			if !errors.As(srvErr, &authErr) {
				t.Errorf("expected ServerAuthError, got: %v", srvErr)
			}
		} else {
			if cliErr != nil {
				t.Fatalf("client: got %s, want no error", cliErr)
			}
		}
	}
}

// Test if authentication attempts are correctly limited on server
// when more public keys are provided then MaxAuthTries
func TestClientAuthMaxAuthTriesPublicKey(t *testing.T) {
	signers := []Signer{}
	for i := 0; i < 6; i++ {
		signers = append(signers, testSigners["dsa"])
	}

	validConfig := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(append([]Signer{testSigners["rsa"]}, signers...)...),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, validConfig); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}

	expectedErr := fmt.Errorf("ssh: handshake failed: %v", &disconnectMsg{
		Reason:  2,
		Message: "too many authentication failures",
	})
	invalidConfig := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			PublicKeys(append(signers, testSigners["rsa"])...),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	if err := tryAuth(t, invalidConfig); err == nil {
		t.Fatalf("client: got no error, want %s", expectedErr)
	} else if err.Error() != expectedErr.Error() {
		// On Windows we can see a WSAECONNABORTED error
		// if the client writes another authentication request
		// before the client goroutine reads the disconnection
		// message.  See issue 50805.
		if runtime.GOOS == "windows" && strings.Contains(err.Error(), "wsarecv: An established connection was aborted") {
			// OK.
		} else {
			t.Fatalf("client: got %s, want %s", err, expectedErr)
		}
	}
}

// Test whether authentication errors are being properly logged if all
// authentication methods have been exhausted
func TestClientAuthErrorList(t *testing.T) {
	publicKeyErr := errors.New("This is an error from PublicKeyCallback")

	clientConfig := &ClientConfig{
		Auth: []AuthMethod{
			PublicKeys(testSigners["rsa"]),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}
	serverConfig := &ServerConfig{
		PublicKeyCallback: func(_ ConnMetadata, _ PublicKey) (*Permissions, error) {
			return nil, publicKeyErr
		},
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	go NewClientConn(c2, "", clientConfig)
	_, err = newServer(c1, serverConfig)
	if err == nil {
		t.Fatal("newServer: got nil, expected errors")
	}

	authErrs, ok := err.(*ServerAuthError)
	if !ok {
		t.Fatalf("errors: got %T, want *ssh.ServerAuthError", err)
	}
	for i, e := range authErrs.Errors {
		switch i {
		case 0:
			if e != ErrNoAuth {
				t.Fatalf("errors: got error %v, want ErrNoAuth", e)
			}
		case 1:
			if e != publicKeyErr {
				t.Fatalf("errors: got %v, want %v", e, publicKeyErr)
			}
		default:
			t.Fatalf("errors: got %v, expected 2 errors", authErrs.Errors)
		}
	}
}

func TestAuthMethodGSSAPIWithMIC(t *testing.T) {
	type testcase struct {
		config        *ClientConfig
		gssConfig     *GSSAPIWithMICConfig
		clientWantErr string
		serverWantErr string
	}
	testcases := []*testcase{
		{
			config: &ClientConfig{
				User: "testuser",
				Auth: []AuthMethod{
					GSSAPIWithMICAuthMethod(
						&FakeClient{
							exchanges: []*exchange{
								{
									outToken: "client-valid-token-1",
								},
								{
									expectedToken: "server-valid-token-1",
								},
							},
							mic:      []byte("valid-mic"),
							maxRound: 2,
						}, "testtarget",
					),
				},
				HostKeyCallback: InsecureIgnoreHostKey(),
			},
			gssConfig: &GSSAPIWithMICConfig{
				AllowLogin: func(conn ConnMetadata, srcName string) (*Permissions, error) {
					if srcName != conn.User()+"@DOMAIN" {
						return nil, fmt.Errorf("srcName is %s, conn user is %s", srcName, conn.User())
					}
					return nil, nil
				},
				Server: &FakeServer{
					exchanges: []*exchange{
						{
							outToken:      "server-valid-token-1",
							expectedToken: "client-valid-token-1",
						},
					},
					maxRound:    1,
					expectedMIC: []byte("valid-mic"),
					srcName:     "testuser@DOMAIN",
				},
			},
		},
		{
			config: &ClientConfig{
				User: "testuser",
				Auth: []AuthMethod{
					GSSAPIWithMICAuthMethod(
						&FakeClient{
							exchanges: []*exchange{
								{
									outToken: "client-valid-token-1",
								},
								{
									expectedToken: "server-valid-token-1",
								},
							},
							mic:      []byte("valid-mic"),
							maxRound: 2,
						}, "testtarget",
					),
				},
				HostKeyCallback: InsecureIgnoreHostKey(),
			},
			gssConfig: &GSSAPIWithMICConfig{
				AllowLogin: func(conn ConnMetadata, srcName string) (*Permissions, error) {
					return nil, fmt.Errorf("user is not allowed to login")
				},
				Server: &FakeServer{
					exchanges: []*exchange{
						{
							outToken:      "server-valid-token-1",
							expectedToken: "client-valid-token-1",
						},
					},
					maxRound:    1,
					expectedMIC: []byte("valid-mic"),
					srcName:     "testuser@DOMAIN",
				},
			},
			serverWantErr: "user is not allowed to login",
			clientWantErr: "ssh: handshake failed: ssh: unable to authenticate",
		},
		{
			config: &ClientConfig{
				User: "testuser",
				Auth: []AuthMethod{
					GSSAPIWithMICAuthMethod(
						&FakeClient{
							exchanges: []*exchange{
								{
									outToken: "client-valid-token-1",
								},
								{
									expectedToken: "server-valid-token-1",
								},
							},
							mic:      []byte("valid-mic"),
							maxRound: 2,
						}, "testtarget",
					),
				},
				HostKeyCallback: InsecureIgnoreHostKey(),
			},
			gssConfig: &GSSAPIWithMICConfig{
				AllowLogin: func(conn ConnMetadata, srcName string) (*Permissions, error) {
					if srcName != conn.User() {
						return nil, fmt.Errorf("srcName is %s, conn user is %s", srcName, conn.User())
					}
					return nil, nil
				},
				Server: &FakeServer{
					exchanges: []*exchange{
						{
							outToken:      "server-invalid-token-1",
							expectedToken: "client-valid-token-1",
						},
					},
					maxRound:    1,
					expectedMIC: []byte("valid-mic"),
					srcName:     "testuser@DOMAIN",
				},
			},
			clientWantErr: "ssh: handshake failed: got \"server-invalid-token-1\", want token \"server-valid-token-1\"",
		},
		{
			config: &ClientConfig{
				User: "testuser",
				Auth: []AuthMethod{
					GSSAPIWithMICAuthMethod(
						&FakeClient{
							exchanges: []*exchange{
								{
									outToken: "client-valid-token-1",
								},
								{
									expectedToken: "server-valid-token-1",
								},
							},
							mic:      []byte("invalid-mic"),
							maxRound: 2,
						}, "testtarget",
					),
				},
				HostKeyCallback: InsecureIgnoreHostKey(),
			},
			gssConfig: &GSSAPIWithMICConfig{
				AllowLogin: func(conn ConnMetadata, srcName string) (*Permissions, error) {
					if srcName != conn.User() {
						return nil, fmt.Errorf("srcName is %s, conn user is %s", srcName, conn.User())
					}
					return nil, nil
				},
				Server: &FakeServer{
					exchanges: []*exchange{
						{
							outToken:      "server-valid-token-1",
							expectedToken: "client-valid-token-1",
						},
					},
					maxRound:    1,
					expectedMIC: []byte("valid-mic"),
					srcName:     "testuser@DOMAIN",
				},
			},
			serverWantErr: "got MICToken \"invalid-mic\", want \"valid-mic\"",
			clientWantErr: "ssh: handshake failed: ssh: unable to authenticate",
		},
	}

	for i, c := range testcases {
		clientErr, serverErrs := tryAuthBothSides(t, c.config, c.gssConfig)
		if (c.clientWantErr == "") != (clientErr == nil) {
			t.Fatalf("client got %v, want %s, case %d", clientErr, c.clientWantErr, i)
		}
		if (c.serverWantErr == "") != (len(serverErrs) == 2 && serverErrs[1] == nil || len(serverErrs) == 1) {
			t.Fatalf("server got err %v, want %s", serverErrs, c.serverWantErr)
		}
		if c.clientWantErr != "" {
			if clientErr != nil && !strings.Contains(clientErr.Error(), c.clientWantErr) {
				t.Fatalf("client  got %v, want %s, case %d", clientErr, c.clientWantErr, i)
			}
		}
		found := false
		var errStrings []string
		if c.serverWantErr != "" {
			for _, err := range serverErrs {
				found = found || (err != nil && strings.Contains(err.Error(), c.serverWantErr))
				errStrings = append(errStrings, err.Error())
			}
			if !found {
				t.Errorf("server got error %q, want substring %q, case %d", errStrings, c.serverWantErr, i)
			}
		}
	}
}

type maliciousGSSAPIWithMICAuthMethod struct{}

func (maliciousGSSAPIWithMICAuthMethod) method() string {
	// Reuse an advertised auth method name so the client attempts this
	// AuthMethod even when gssapi-with-mic is not advertised.
	return "password"
}

func (maliciousGSSAPIWithMICAuthMethod) auth(session []byte, user string, c packetConn, rand io.Reader, _ map[string][]byte) (authResult, []string, error) {
	req := &userAuthRequestMsg{
		User:    user,
		Service: serviceSSH,
		Method:  "gssapi-with-mic",
	}
	req.Payload = appendU32(req.Payload, 1)
	req.Payload = appendString(req.Payload, string(krb5OID))
	if err := c.writePacket(Marshal(req)); err != nil {
		return authFailure, nil, err
	}

	packet, err := c.readPacket()
	if err != nil {
		return authFailure, nil, err
	}
	switch packet[0] {
	case msgUserAuthFailure:
		var msg userAuthFailureMsg
		if err := Unmarshal(packet, &msg); err != nil {
			return authFailure, nil, err
		}
		if msg.PartialSuccess {
			return authPartialSuccess, msg.Methods, nil
		}
		return authFailure, msg.Methods, nil
	default:
		return authFailure, nil, unexpectedMessageError(msgUserAuthFailure, packet[0])
	}
}

func TestGSSAPIWithMICPartialConfigRejectedAtRuntime(t *testing.T) {
	config := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			maliciousGSSAPIWithMICAuthMethod{},
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	clientErr, serverErrs := tryAuthBothSides(t, config, &GSSAPIWithMICConfig{
		Server: &FakeServer{},
	})

	if clientErr == nil || !strings.Contains(clientErr.Error(), "ssh: unable to authenticate") {
		t.Fatalf("client got %v, want authentication failure", clientErr)
	}

	found := false
	for _, err := range serverErrs {
		if err != nil && strings.Contains(err.Error(), "ssh: gssapi-with-mic auth not configured") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("server auth errors = %v, want gssapi-with-mic auth not configured", serverErrs)
	}
}

func TestCompatibleAlgoAndSignatures(t *testing.T) {
	type testcase struct {
		algo       string
		sigFormat  string
		compatible bool
	}
	testcases := []*testcase{
		{
			KeyAlgoRSA,
			KeyAlgoRSA,
			true,
		},
		{
			KeyAlgoRSA,
			KeyAlgoRSASHA256,
			true,
		},
		{
			KeyAlgoRSA,
			KeyAlgoRSASHA512,
			true,
		},
		{
			KeyAlgoRSASHA256,
			KeyAlgoRSA,
			true,
		},
		{
			KeyAlgoRSASHA512,
			KeyAlgoRSA,
			true,
		},
		{
			KeyAlgoRSASHA512,
			KeyAlgoRSASHA256,
			true,
		},
		{
			KeyAlgoRSASHA256,
			KeyAlgoRSASHA512,
			true,
		},
		{
			KeyAlgoRSASHA512,
			KeyAlgoRSASHA512,
			true,
		},
		{
			CertAlgoRSAv01,
			KeyAlgoRSA,
			true,
		},
		{
			CertAlgoRSAv01,
			KeyAlgoRSASHA256,
			true,
		},
		{
			CertAlgoRSAv01,
			KeyAlgoRSASHA512,
			true,
		},
		{
			CertAlgoRSASHA256v01,
			KeyAlgoRSASHA512,
			true,
		},
		{
			CertAlgoRSASHA512v01,
			KeyAlgoRSASHA512,
			true,
		},
		{
			CertAlgoRSASHA512v01,
			KeyAlgoRSASHA256,
			true,
		},
		{
			CertAlgoRSASHA256v01,
			CertAlgoRSAv01,
			true,
		},
		{
			CertAlgoRSAv01,
			CertAlgoRSASHA512v01,
			true,
		},
		{
			KeyAlgoECDSA256,
			KeyAlgoRSA,
			false,
		},
		{
			KeyAlgoECDSA256,
			KeyAlgoECDSA521,
			false,
		},
		{
			KeyAlgoECDSA256,
			KeyAlgoECDSA256,
			true,
		},
		{
			KeyAlgoECDSA256,
			KeyAlgoED25519,
			false,
		},
		{
			KeyAlgoED25519,
			KeyAlgoED25519,
			true,
		},
	}

	for _, c := range testcases {
		if isAlgoCompatible(c.algo, c.sigFormat) != c.compatible {
			t.Errorf("algorithm %q, signature format %q, expected compatible to be %t", c.algo, c.sigFormat, c.compatible)
		}
	}
}

func TestPickSignatureAlgorithm(t *testing.T) {
	type testcase struct {
		name       string
		extensions map[string][]byte
	}
	cases := []testcase{
		{
			name: "server with empty server-sig-algs",
			extensions: map[string][]byte{
				"server-sig-algs": []byte(``),
			},
		},
		{
			name:       "server with no server-sig-algs",
			extensions: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			signer, ok := testSigners["rsa"].(MultiAlgorithmSigner)
			if !ok {
				t.Fatalf("rsa test signer does not implement the MultiAlgorithmSigner interface")
			}
			// The signer supports the public key algorithm which is then returned.
			_, algo, err := pickSignatureAlgorithm(signer, c.extensions)
			if err != nil {
				t.Fatalf("got %v, want no error", err)
			}
			if algo != signer.PublicKey().Type() {
				t.Fatalf("got algo %q, want %q", algo, signer.PublicKey().Type())
			}
			// Test a signer that uses a certificate algorithm as the public key
			// type.
			cert := &Certificate{
				CertType: UserCert,
				Key:      signer.PublicKey(),
			}
			cert.SignCert(rand.Reader, signer)

			certSigner, err := NewCertSigner(cert, signer)
			if err != nil {
				t.Fatalf("error generating cert signer: %v", err)
			}
			// The signer supports the public key algorithm and the
			// public key format is a certificate type so the certificate
			// algorithm matching the key format must be returned
			_, algo, err = pickSignatureAlgorithm(certSigner, c.extensions)
			if err != nil {
				t.Fatalf("got %v, want no error", err)
			}
			if algo != certSigner.PublicKey().Type() {
				t.Fatalf("got algo %q, want %q", algo, certSigner.PublicKey().Type())
			}
			signer, err = NewSignerWithAlgorithms(signer.(AlgorithmSigner), []string{KeyAlgoRSASHA512, KeyAlgoRSASHA256})
			if err != nil {
				t.Fatalf("unable to create signer with algorithms: %v", err)
			}
			// The signer does not support the public key algorithm so an error
			// is returned.
			_, _, err = pickSignatureAlgorithm(signer, c.extensions)
			if err == nil {
				t.Fatal("got no error, no common public key signature algorithm error expected")
			}
		})
	}
}

func TestPickSignatureAlgorithmRespectsSignerPreference(t *testing.T) {
	algoSigner, ok := testSigners["rsa"].(AlgorithmSigner)
	if !ok {
		t.Fatalf("rsa test signer does not implement the AlgorithmSigner interface")
	}

	serverExtensions := map[string][]byte{
		"server-sig-algs": []byte(KeyAlgoRSASHA256 + "," + KeyAlgoRSASHA512),
	}

	tests := []struct {
		name         string
		signerPrefs  []string
		expectedAlgo string
	}{
		{
			name:         "Signer prefers SHA512 then SHA256",
			signerPrefs:  []string{KeyAlgoRSASHA512, KeyAlgoRSASHA256},
			expectedAlgo: KeyAlgoRSASHA512,
		},
		{
			name:         "Signer prefers SHA256 then SHA512",
			signerPrefs:  []string{KeyAlgoRSASHA256, KeyAlgoRSASHA512},
			expectedAlgo: KeyAlgoRSASHA256,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orderedSigner, err := NewSignerWithAlgorithms(algoSigner, tc.signerPrefs)
			if err != nil {
				t.Fatalf("failed to create ordered signer: %v", err)
			}

			_, selectedAlgo, err := pickSignatureAlgorithm(orderedSigner, serverExtensions)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if selectedAlgo != tc.expectedAlgo {
				t.Errorf("Algorithm mismatch; got %q want %q", selectedAlgo, tc.expectedAlgo)
			}
		})
	}
}

// configurablePublicKeyCallback is a public key callback that allows to
// configure the signature algorithm and format. This way we can emulate the
// behavior of buggy clients.
type configurablePublicKeyCallback struct {
	signer          AlgorithmSigner
	signatureAlgo   string
	signatureFormat string
}

func (cb configurablePublicKeyCallback) method() string {
	return "publickey"
}

func (cb configurablePublicKeyCallback) auth(session []byte, user string, c packetConn, rand io.Reader, extensions map[string][]byte) (authResult, []string, error) {
	pub := cb.signer.PublicKey()

	ok, err := validateKey(pub, cb.signatureAlgo, user, c)
	if err != nil {
		return authFailure, nil, err
	}
	if !ok {
		return authFailure, nil, fmt.Errorf("invalid public key")
	}

	pubKey := pub.Marshal()
	data := buildDataSignedForAuth(session, userAuthRequestMsg{
		User:    user,
		Service: serviceSSH,
		Method:  cb.method(),
	}, cb.signatureAlgo, pubKey)
	sign, err := cb.signer.SignWithAlgorithm(rand, data, underlyingAlgo(cb.signatureFormat))
	if err != nil {
		return authFailure, nil, err
	}

	s := Marshal(sign)
	sig := make([]byte, stringLength(len(s)))
	marshalString(sig, s)
	msg := publickeyAuthMsg{
		User:     user,
		Service:  serviceSSH,
		Method:   cb.method(),
		HasSig:   true,
		Algoname: cb.signatureAlgo,
		PubKey:   pubKey,
		Sig:      sig,
	}
	p := Marshal(&msg)
	if err := c.writePacket(p); err != nil {
		return authFailure, nil, err
	}
	var success authResult
	success, methods, err := handleAuthResponse(c)
	if err != nil {
		return authFailure, nil, err
	}
	if success == authSuccess || !slices.Contains(methods, cb.method()) {
		return success, methods, err
	}

	return authFailure, methods, nil
}

func TestPublicKeyAndAlgoCompatibility(t *testing.T) {
	cert := &Certificate{
		Key:         testPublicKeys["rsa"],
		ValidBefore: CertTimeInfinity,
		CertType:    UserCert,
	}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	certSigner, err := NewCertSigner(cert, testSigners["rsa"])
	if err != nil {
		t.Fatalf("NewCertSigner: %v", err)
	}

	clientConfig := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		Auth: []AuthMethod{
			configurablePublicKeyCallback{
				signer:          certSigner.(AlgorithmSigner),
				signatureAlgo:   KeyAlgoRSASHA256,
				signatureFormat: KeyAlgoRSASHA256,
			},
		},
	}
	if err := tryAuth(t, clientConfig); err == nil {
		t.Error("cert login passed with incompatible public key type and algorithm")
	}
}

func TestClientAuthGPGAgentCompat(t *testing.T) {
	clientConfig := &ClientConfig{
		User:            "testuser",
		HostKeyCallback: InsecureIgnoreHostKey(),
		Auth: []AuthMethod{
			// algorithm rsa-sha2-512 and signature format ssh-rsa.
			configurablePublicKeyCallback{
				signer:          testSigners["rsa"].(AlgorithmSigner),
				signatureAlgo:   KeyAlgoRSASHA512,
				signatureFormat: KeyAlgoRSA,
			},
		},
	}
	if err := tryAuth(t, clientConfig); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestCertAuthOpenSSHCompat(t *testing.T) {
	cert := &Certificate{
		Key:         testPublicKeys["rsa"],
		ValidBefore: CertTimeInfinity,
		CertType:    UserCert,
	}
	cert.SignCert(rand.Reader, testSigners["ecdsa"])
	certSigner, err := NewCertSigner(cert, testSigners["rsa"])
	if err != nil {
		t.Fatalf("NewCertSigner: %v", err)
	}

	clientConfig := &ClientConfig{
		User:            "user",
		HostKeyCallback: InsecureIgnoreHostKey(),
		Auth: []AuthMethod{
			// algorithm ssh-rsa-cert-v01@openssh.com and signature format
			// rsa-sha2-256.
			configurablePublicKeyCallback{
				signer:          certSigner.(AlgorithmSigner),
				signatureAlgo:   CertAlgoRSAv01,
				signatureFormat: KeyAlgoRSASHA256,
			},
		},
	}
	if err := tryAuth(t, clientConfig); err != nil {
		t.Fatalf("unable to dial remote side: %s", err)
	}
}

func TestKeyboardInteractiveAuthEarlyFail(t *testing.T) {
	const maxAuthTries = 2

	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	// Start testserver
	serverConfig := &ServerConfig{
		MaxAuthTries: maxAuthTries,
		KeyboardInteractiveCallback: func(c ConnMetadata,
			client KeyboardInteractiveChallenge) (*Permissions, error) {
			// Fail keyboard-interactive authentication early before
			// any prompt is sent to client.
			return nil, errors.New("keyboard-interactive auth failed")
		},
		PasswordCallback: func(c ConnMetadata,
			pass []byte) (*Permissions, error) {
			if string(pass) == clientPassword {
				return nil, nil
			}
			return nil, errors.New("password auth failed")
		},
	}
	serverConfig.AddHostKey(testSigners["rsa"])

	serverDone := make(chan struct{})
	go func() {
		defer func() { serverDone <- struct{}{} }()
		conn, chans, reqs, err := NewServerConn(c2, serverConfig)
		if err != nil {
			return
		}
		_ = conn.Close()

		discarderDone := make(chan struct{})
		go func() {
			defer func() { discarderDone <- struct{}{} }()
			DiscardRequests(reqs)
		}()
		for newChannel := range chans {
			newChannel.Reject(Prohibited,
				"testserver not accepting requests")
		}

		<-discarderDone
	}()

	// Connect to testserver, expect KeyboardInteractive() to be not called,
	// PasswordCallback() to be called and connection to succeed.
	passwordCallbackCalled := false
	clientConfig := &ClientConfig{
		User: "testuser",
		Auth: []AuthMethod{
			RetryableAuthMethod(KeyboardInteractive(func(name,
				instruction string, questions []string,
				echos []bool) ([]string, error) {
				t.Errorf("unexpected call to KeyboardInteractive()")
				return []string{clientPassword}, nil
			}), maxAuthTries),
			RetryableAuthMethod(PasswordCallback(func() (secret string,
				err error) {
				t.Logf("PasswordCallback()")
				passwordCallbackCalled = true
				return clientPassword, nil
			}), maxAuthTries),
		},
		HostKeyCallback: InsecureIgnoreHostKey(),
	}

	conn, _, _, err := NewClientConn(c1, "", clientConfig)
	if err != nil {
		t.Errorf("unexpected NewClientConn() error: %v", err)
	}
	if conn != nil {
		conn.Close()
	}

	// Wait for server to finish.
	<-serverDone

	if !passwordCallbackCalled {
		t.Errorf("expected PasswordCallback() to be called")
	}
}
