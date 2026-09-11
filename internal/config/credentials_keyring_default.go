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
// The platform Get is not context-aware on these OSes, so the lookup is run
// under the shared migration budget via legacyKeyringGetBounded: a wedged OS
// keyring (locked macOS login keychain, hung Windows Credential Manager) must
// not hang `serve` forever, and headless boxes must not stall startup.
func legacyKeyringProbe(ctx context.Context, key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	if err := ctx.Err(); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}
	value, err, timedOut := legacyKeyringGetBounded(ctx, func() (string, error) {
		return keyring.Get(credentialsKeyringService, key)
	})
	if timedOut {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}
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
