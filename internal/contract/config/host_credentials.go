package config

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const hostCredentialPrefix = "REASONIX_HOST_"

// IsHostCredentialSlot identifies values that must never enter process env.
func IsHostCredentialSlot(slot string) bool {
	id, ok := strings.CutPrefix(slot, hostCredentialPrefix)
	id, suffix := strings.CutSuffix(id, "_SECRET")
	if !ok || !suffix || len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// SaveHostCredential creates an immutable private slot without exporting its
// value. The file backend has the same at-rest limits as other credentials.
func SaveHostCredential(value string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	slot := fmt.Sprintf("%s%X_SECRET", hostCredentialPrefix, id)
	unlock, err := LockUserCredentialEdits()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := storeCredentialsInFile(UserCredentialsPath(), map[string]string{slot: value}); err != nil {
		return "", err
	}
	return slot, nil
}

// ReadHostCredential reads only the host store; environment and project values
// cannot substitute for the slot a host recorded.
func ReadHostCredential(slot string) (string, bool) {
	if !IsHostCredentialSlot(slot) {
		return "", false
	}
	return envFileValue(UserCredentialsPath(), slot)
}
