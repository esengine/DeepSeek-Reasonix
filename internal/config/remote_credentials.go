package config

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	remotePasswordCredentialKind   = "PASSWORD"
	remotePassphraseCredentialKind = "KEY_PASSPHRASE"
)

// CredentialChange is one mutation in the user-global credential store. The
// value is never written into config.toml; only its environment-key reference
// belongs in a RemoteHostEntry.
type CredentialChange struct {
	Key    string
	Value  string
	Remove bool
}

type remoteCredentialSnapshot struct {
	value string
	set   bool
}

// RemotePasswordCredentialEnvName returns the Reasonix-owned credential slot
// for a remote host password without exposing the user-supplied label.
func RemotePasswordCredentialEnvName(hostID string) string {
	return remoteCredentialEnvName(hostID, remotePasswordCredentialKind)
}

// RemotePassphraseCredentialEnvName returns the Reasonix-owned credential slot
// for a remote host private-key passphrase.
func RemotePassphraseCredentialEnvName(hostID string) string {
	return remoteCredentialEnvName(hostID, remotePassphraseCredentialKind)
}

func remoteCredentialEnvName(hostID, kind string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(hostID)))
	return fmt.Sprintf("REASONIX_REMOTE_%X_%s", sum[:8], kind)
}

// IsGeneratedRemoteCredential reports whether key is a Reasonix-owned slot
// for this host. User-managed/shared environment variables are never deleted.
func IsGeneratedRemoteCredential(hostID, key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && (key == RemotePasswordCredentialEnvName(hostID) ||
		key == RemotePassphraseCredentialEnvName(hostID))
}

// UnusedGeneratedRemoteCredentialChanges returns deduplicated removals for
// candidates that are no longer referenced by any configured remote host.
func UnusedGeneratedRemoteCredentialChanges(c *Config, candidates []string) []CredentialChange {
	if c == nil || len(candidates) == 0 {
		return nil
	}
	used := make(map[string]bool, len(c.Remote.Hosts)*2)
	for _, host := range c.Remote.Hosts {
		used[strings.TrimSpace(host.PasswordEnv)] = true
		used[strings.TrimSpace(host.PassphraseEnv)] = true
	}
	seen := map[string]bool{}
	changes := make([]CredentialChange, 0, len(candidates))
	for _, key := range candidates {
		key = strings.TrimSpace(key)
		if key == "" || used[key] || seen[key] {
			continue
		}
		seen[key] = true
		changes = append(changes, CredentialChange{Key: key, Remove: true})
	}
	return changes
}

// EditUserConfigWithCredentials updates config and its Reasonix-owned secret
// slots as one recoverable operation. Credential writes happen before SaveTo;
// any later failure restores every touched slot, keeping plaintext out of TOML.
func EditUserConfigWithCredentials(mutate func(*Config) ([]CredentialChange, error)) error {
	unlock := LockUserConfigEdits()
	defer unlock()
	path := UserConfigPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cannot resolve user config path")
	}
	cfg := LoadForEdit(path)
	if cfg == nil {
		cfg = Default()
	}
	changes, err := mutate(cfg)
	if err != nil {
		return err
	}
	snapshots := map[string]remoteCredentialSnapshot{}
	applied := make([]string, 0, len(changes))
	rollback := func() {
		seen := map[string]bool{}
		for i := len(applied) - 1; i >= 0; i-- {
			key := applied[i]
			if seen[key] {
				continue
			}
			seen[key] = true
			snapshot := snapshots[key]
			if snapshot.set {
				_, _ = SetCredential(key, snapshot.value)
			} else {
				_ = RemoveCredential(key)
			}
		}
	}
	for _, change := range changes {
		change.Key = strings.TrimSpace(change.Key)
		if change.Key == "" {
			continue
		}
		if _, ok := snapshots[change.Key]; !ok {
			resolved := ResolveCredentialForRootGlobalFirst(".", change.Key)
			snapshots[change.Key] = remoteCredentialSnapshot{value: resolved.Value, set: resolved.Set}
		}
		if change.Remove {
			err = RemoveCredential(change.Key)
		} else {
			_, err = SetCredential(change.Key, change.Value)
		}
		if err != nil {
			rollback()
			return fmt.Errorf("update remote credential %s: %w", change.Key, err)
		}
		applied = append(applied, change.Key)
	}
	if err := cfg.SaveTo(path); err != nil {
		rollback()
		return err
	}
	return nil
}
