package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// credentialsPath is the reasonix-owned global secrets file the settings panel
// writes API keys to — the same file `reasonix setup` writes and config.loadDotEnv
// reads, so a key set in the desktop app resolves for the CLI from any directory.
// Never a project .env: keys stay out of the user's project tree. Falls back to
// ~/.env only when the user config dir can't be resolved.
func credentialsPath() string {
	if p := config.UserCredentialsPath(); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".env")
	}
	return ".env"
}

// upsertDotEnv sets KEY=value in the global credentials file (replacing an
// existing KEY line, else appending), and applies it to the running process so a
// rebuild picks it up without a restart.
func upsertDotEnv(key, value string) error {
	return upsertEnvFile(credentialsPath(), key, value)
}

func removeDotEnv(key string) error {
	return removeEnvFile(credentialsPath(), key)
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

// envFileKeys returns the set of KEY names assigned in a KEY=value file, empty
// when the file is absent.
func envFileKeys(path string) map[string]bool {
	keys := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return keys
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, _, ok := strings.Cut(line, "="); ok {
			keys[strings.TrimSpace(k)] = true
		}
	}
	return keys
}

// promoteProviderKeysToCredentials copies any configured provider api_key_env that
// currently resolves (from a project .env, ~/.env, or the OS env) into the global
// credentials file when it isn't there yet, so a key set for one workspace follows
// the user across every project. Promoted keys are then stripped from ~/.env so the
// credentials file is the single source of truth; a project's own .env is
// user-owned and left untouched.
func promoteProviderKeysToCredentials(cfg *config.Config) {
	credPath := credentialsPath()
	have := envFileKeys(credPath)
	for _, p := range cfg.Providers {
		env := strings.TrimSpace(p.APIKeyEnv)
		if env == "" || have[env] {
			continue
		}
		val := os.Getenv(env)
		if val == "" {
			continue
		}
		if err := upsertEnvFile(credPath, env, val); err != nil {
			continue
		}
		have[env] = true
		removeHomeEnvKey(env)
	}
}

// removeHomeEnvKey deletes a single KEY=value assignment from ~/.env (the legacy
// fallback the old migration wrote to), leaving every other line intact. No-op when
// ~/.env is absent or the credentials store resolves to ~/.env itself.
func removeHomeEnvKey(key string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".env")
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
