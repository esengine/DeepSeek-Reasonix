//go:build !linux && !netbsd && !openbsd && !(dragonfly && cgo) && !(freebsd && cgo)

package config

import (
	"strings"

	"github.com/zalando/go-keyring"
)

func legacyKeyringCredentialValue(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	value, err := keyring.Get(credentialsKeyringService, key)
	return value, err == nil && value != ""
}
