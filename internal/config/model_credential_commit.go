package config

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
)

// StageModelCredential creates a private reference before the config commit.
// Callers hold both config and credential edit locks and defer cleanup
// through validation and persistence. No existing credential is overwritten.
func (c *Config) StageModelCredentialLocked(value string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	key := fmt.Sprintf("REASONIX_CONNECTION_%X_KEY", id)
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("credential value contains a newline")
	}
	if _, err := storeCredentialAssignmentsLocked(map[string]string{key: value}); err != nil {
		return "", err
	}
	c.stagedModelCredentials = append(c.stagedModelCredentials, key)
	return key, nil
}

// CleanupStagedModelCredentials only inspects references minted by this edit.
// Both edit locks still belong to the caller. An uncertain read or failed
// cleanup conservatively leaves an orphan; it never damages the old connection.
func (c *Config) CleanupStagedModelCredentialsLocked(path string) {
	if len(c.stagedModelCredentials) == 0 {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	for _, key := range c.stagedModelCredentials {
		// Check the entire document to retain references in unknown fields too.
		if !bytes.Contains(raw, []byte(key)) {
			if err := removeCredentialFromFile(UserCredentialsPath(), key); err == nil {
				_ = os.Unsetenv(key)
			}
		}
	}
	c.stagedModelCredentials = nil
}
