package remotecloud

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const protocolVersion = 1

type hello struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	PublicKey string `json:"publicKey"`
	Salt      string `json:"salt"`
}

type sealedMessage struct {
	Version    int    `json:"v"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type sessionCipher struct {
	aead cipher.AEAD
	aad  []byte
	seen map[string]struct{}
}

func newSessionCipher(private *ecdh.PrivateKey, deviceID, payload string) (*sessionCipher, error) {
	var greeting hello
	if err := json.Unmarshal([]byte(payload), &greeting); err != nil {
		return nil, err
	}
	if greeting.Version != protocolVersion || greeting.Type != "hello" {
		return nil, errors.New("remote cloud: unsupported handshake")
	}
	peerBytes, err := base64.RawURLEncoding.DecodeString(greeting.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("remote cloud: controller public key: %w", err)
	}
	peer, err := ecdh.X25519().NewPublicKey(peerBytes)
	if err != nil {
		return nil, fmt.Errorf("remote cloud: controller public key: %w", err)
	}
	salt, err := base64.RawURLEncoding.DecodeString(greeting.Salt)
	if err != nil || len(salt) != 16 {
		return nil, errors.New("remote cloud: invalid handshake salt")
	}
	secret, err := private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("remote cloud: key agreement: %w", err)
	}
	info := "reasonix-remote-v1|" + deviceID
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, salt, []byte(info)), key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sessionCipher{aead: aead, aad: []byte(info), seen: make(map[string]struct{})}, nil
}

func (s *sessionCipher) seal(value any) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	wire := sealedMessage{
		Version:    protocolVersion,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(s.aead.Seal(nil, nonce, plain, s.aad)),
	}
	encoded, err := json.Marshal(wire)
	return string(encoded), err
}

func (s *sessionCipher) open(payload string, out any) error {
	var wire sealedMessage
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return err
	}
	if wire.Version != protocolVersion {
		return errors.New("remote cloud: unsupported message version")
	}
	if _, duplicate := s.seen[wire.Nonce]; duplicate {
		return errors.New("remote cloud: replayed message")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(wire.Nonce)
	if err != nil || len(nonce) != s.aead.NonceSize() {
		return errors.New("remote cloud: invalid message nonce")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(wire.Ciphertext)
	if err != nil {
		return errors.New("remote cloud: invalid ciphertext")
	}
	plain, err := s.aead.Open(nil, nonce, ciphertext, s.aad)
	if err != nil {
		return errors.New("remote cloud: message authentication failed")
	}
	if len(s.seen) >= 256 {
		clear(s.seen)
	}
	s.seen[wire.Nonce] = struct{}{}
	return json.Unmarshal(plain, out)
}
