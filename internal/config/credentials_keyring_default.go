//go:build !linux && !netbsd && !openbsd && !(dragonfly && cgo) && !(freebsd && cgo)

package config

import (
	"context"
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

// legacyKeyringProbe reads one legacy credential from the platform keyring.
// keyring.ErrNotFound (and empty values) are absent; other errors stay error.
// ctx is checked before the call so a shared batch budget can stop the scan;
// platform Get is not context-aware on these OSes.
func legacyKeyringProbe(ctx context.Context, key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	if err := ctx.Err(); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}
	value, err := keyring.Get(credentialsKeyringService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
		if ctx.Err() != nil {
			return legacyKeyringOutcome{Status: legacyKeyringTimeout}
		}
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	if strings.TrimSpace(value) == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	return legacyKeyringOutcome{Status: legacyKeyringFound, Value: value}
}
