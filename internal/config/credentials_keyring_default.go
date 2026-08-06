//go:build !linux && !netbsd && !openbsd && !(dragonfly && cgo) && !(freebsd && cgo)

package config

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

// legacyKeyringProbe reads one legacy credential from the platform keyring.
// keyring.ErrNotFound (and empty values) are absent; other errors stay error.
func legacyKeyringProbe(key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	value, err := keyring.Get(credentialsKeyringService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	if strings.TrimSpace(value) == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	return legacyKeyringOutcome{Status: legacyKeyringFound, Value: value}
}
