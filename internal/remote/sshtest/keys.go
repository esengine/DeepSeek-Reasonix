package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

// generateHostKey creates a fresh ed25519 SSH signer for the test server.
func generateHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// GenerateKeyPair returns a new ed25519 signer and its public key, for tests
// that exercise publickey auth.
func GenerateKeyPair() (ssh.Signer, ssh.PublicKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return signer, signer.PublicKey(), nil
}

// GenerateKeyPEM returns an unencrypted OpenSSH private key PEM alongside its
// public key, for tests that need an identity file on disk.
func GenerateKeyPEM() (pemBytes []byte, pub ssh.PublicKey, err error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(block), signer.PublicKey(), nil
}

// GenerateEncryptedKeyPEM returns an encrypted OpenSSH private key PEM and its
// public key, for tests that exercise passphrase-backed identity files.
func GenerateEncryptedKeyPEM(passphrase string) (pemBytes []byte, pub ssh.PublicKey, err error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(block), signer.PublicKey(), nil
}
