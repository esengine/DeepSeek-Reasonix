//go:build (dragonfly && cgo) || (freebsd && cgo) || linux || netbsd || openbsd

package config

import (
	"strings"

	ss "github.com/zalando/go-keyring/secret_service"
)

// legacyKeyringProbe reads one legacy credential from Secret Service.
// Connection/Unlock/Search/session failures return error (no marker).
// Empty search results or empty secret values return absent (marker OK).
func legacyKeyringProbe(key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}

	svc, err := ss.NewSecretService()
	if err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	defer svc.Conn.Close()

	collection := svc.GetLoginCollection()
	search := map[string]string{
		"username": key,
		"service":  credentialsKeyringService,
	}
	if err := svc.Unlock(collection.Path()); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	results, err := svc.SearchItems(collection, search)
	if err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	if len(results) == 0 {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}

	session, err := svc.OpenSession()
	if err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	defer svc.Close(session)

	if err := svc.Unlock(results[0]); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	secret, err := svc.GetSecret(results[0], session.Path())
	if err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	if secret == nil || len(secret.Value) == 0 {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	return legacyKeyringOutcome{Status: legacyKeyringFound, Value: string(secret.Value)}
}
