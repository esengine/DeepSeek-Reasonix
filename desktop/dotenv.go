package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// credentialsPath is the reasonix-owned global .env used for provider keys.
func credentialsPath() string {
	if p := config.UserCredentialsPath(); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".env")
	}
	return ".env"
}

// upsertDotEnv stores KEY=value in Reasonix's global .env and applies it to the
// running process so a rebuild picks it up without a restart.
func upsertDotEnv(key, value string) error {
	_, err := config.SetCredential(key, value)
	return err
}

func removeDotEnv(key string) error {
	return config.RemoveCredential(key)
}

// upsertEnvFile merges KEY=value into a KEY=value file at path, preserving
// comments and unrelated lines, writing atomically via a sibling temp + rename.
func upsertEnvFile(path, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	replaced := false
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, key+"="+value)
	}
	out := strings.Join(lines, "\n") + "\n"

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Setenv(key, value)
}

func removeEnvFile(path, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Unsetenv(key)
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	outLines := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "export "))
		if t == "" || strings.HasPrefix(t, "#") {
			outLines = append(outLines, ln)
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			continue
		}
		outLines = append(outLines, ln)
	}
	out := ""
	if len(outLines) > 0 {
		out = strings.Join(outLines, "\n") + "\n"
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "credentials.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Unsetenv(key)
}

// promoteProviderKeysToCredentials copies provider keys from the legacy ~/.env
// bridge into Reasonix's global .env when a key is not there yet. It intentionally
// ignores inherited process env vars so runtime provider resolution remains
// rooted in the Reasonix-owned global .env.
func promoteProviderKeysToCredentials(cfg *config.Config) {
	homeEnv := legacyHomeEnvPath()
	for _, p := range cfg.Providers {
		env := strings.TrimSpace(p.APIKeyEnv)
		if env == "" || config.CredentialStored(env) {
			continue
		}
		val, ok := envFileValue(homeEnv, env)
		if !ok || val == "" {
			continue
		}
		if _, err := config.SetCredential(env, val); err != nil {
			continue
		}
		removeHomeEnvKey(env)
	}
}

func legacyHomeEnvPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".env")
}

func envFileValue(path, wantKey string) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "export "))
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, value, ok := strings.Cut(t, "=")
		if !ok || strings.TrimSpace(key) != wantKey {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`), true
	}
	return "", false
}

// removeHomeEnvKey deletes a single KEY=value assignment from ~/.env (the legacy
// fallback the old migration wrote to), leaving every other line intact. No-op when
// ~/.env is absent or the credentials store resolves to ~/.env itself.
func removeHomeEnvKey(key string) {
	path := legacyHomeEnvPath()
	if path == "" {
		return
	}
	if sameConfigPath(path, credentialsPath()) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	removed := false
	for _, raw := range strings.Split(string(data), "\n") {
		check := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
		if k, _, ok := strings.Cut(check, "="); ok && strings.TrimSpace(k) == key {
			removed = true
			continue
		}
		kept = append(kept, raw)
	}
	if !removed {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600)
}
