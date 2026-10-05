package account

import (
	"strings"

	"reasonix/internal/contract/config"
)

// TokenKey is where the session token lives in the credential store, so it
// inherits the same file permissions, edit lock and environment override every
// other Reasonix secret has — and a headless host can supply it as an env var
// without a login round trip.
const TokenKey = "REASONIX_ACCOUNT_TOKEN"

// Token returns the stored session token, or "" when signed out.
func Token() string {
	return strings.TrimSpace(config.ResolveCredential(TokenKey).Value)
}

// SaveToken persists the session token and reports where it landed.
func SaveToken(token string) (string, error) {
	return config.SetCredential(TokenKey, strings.TrimSpace(token))
}

// ClearToken forgets the session locally. It touches nothing else: sessions,
// memory and configuration are the user's and survive signing out.
func ClearToken() error {
	return config.RemoveCredential(TokenKey)
}
