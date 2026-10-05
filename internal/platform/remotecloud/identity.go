package remotecloud

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/contract/config"
)

const identityCredentialKey = "REASONIX_REMOTE_DEVICE_IDENTITY"

type identity struct {
	OwnerID          int64  `json:"ownerId"`
	DeviceID         string `json:"deviceId"`
	DeviceCredential string `json:"deviceCredential"`
	PrivateKey       string `json:"privateKey"`
}

func loadIdentity() (*identity, error) {
	raw := strings.TrimSpace(config.ResolveCredential(identityCredentialKey).Value)
	if raw == "" {
		return nil, nil
	}
	var saved identity
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, err
	}
	if saved.OwnerID < 1 || saved.DeviceID == "" || saved.DeviceCredential == "" || saved.PrivateKey == "" {
		return nil, errors.New("remote cloud: saved device identity is incomplete")
	}
	return &saved, nil
}

func saveIdentity(saved *identity) error {
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	_, err = config.SetCredential(identityCredentialKey, string(raw))
	return err
}

func clearIdentity() error { return config.RemoveCredential(identityCredentialKey) }

func generatePrivateKey() (*ecdh.PrivateKey, string, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	return private, base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), nil
}

func privateKey(saved *identity) (*ecdh.PrivateKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(saved.PrivateKey)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}
