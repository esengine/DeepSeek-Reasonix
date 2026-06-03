package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/fileutil"
)

// dotEnvPath is ~/.env (absolute, so a workspace switch — which changes cwd —
// can't scatter or lose the key).
var dotEnvPath = func() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".env")
	}
	return ".env" // fallback: relative to cwd if home is unavailable
}()

// upsertDotEnv sets KEY=value in the home-directory .env (replacing an existing
// KEY line, else appending), and applies it to the running process so a rebuild
// picks it up without a restart. Comments and unrelated lines are preserved.
func upsertDotEnv(key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	var lines []string
	if b, err := os.ReadFile(dotEnvPath); err == nil {
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

	tmp, err := os.CreateTemp(filepath.Dir(dotEnvPath), ".env.*.tmp")
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
	if err := fileutil.ReplaceFile(tmpPath, dotEnvPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Setenv(key, value)
}
