package team

import (
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/teamroster"
)

type secretCredentials struct{}

// WithSecretCredentials binds the host's global credential store. Its current
// backend is a private .env file, not encrypted storage or same-user isolation.
func WithSecretCredentials(store teamroster.Store) teamroster.Store {
	if store.Credentials == nil {
		store.Credentials = secretCredentials{}
		secrets.RegisterHostSecretPath(config.UserCredentialsPath())
	}
	return store
}

func (secretCredentials) Save(value string) (string, error) {
	slot, err := config.SaveHostCredential(value)
	if err == nil {
		secrets.RegisterCredentialEnvKeys([]string{slot})
	}
	return slot, err
}

func (secretCredentials) Read(slot string) (string, error) {
	if !config.IsHostCredentialSlot(slot) {
		return "", teamroster.ErrSecretCredentials
	}
	secrets.RegisterCredentialEnvKeys([]string{slot})
	value, ok := config.ReadHostCredential(slot)
	if !ok {
		return "", teamroster.ErrSecretCredentials
	}
	return value, nil
}
