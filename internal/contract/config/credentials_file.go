package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joho/godotenv"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
)

func storeCredentialsInFile(path string, assignments map[string]string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("credentials store unavailable")
	}
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return err
	}
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if key, ok := credentialClearedLineKey(line); ok {
			if _, hit := assignments[key]; hit {
				continue
			}
		}
		filtered = append(filtered, line)
	}
	lines = filtered
	replaced := map[string]bool{}
	for i, line := range lines {
		key, ok := credentialLineKey(line)
		if !ok {
			continue
		}
		if value, hit := assignments[key]; hit {
			lines[i] = formatCredentialLine(key, value)
			replaced[key] = true
		}
	}
	keys := slices.Sorted(maps.Keys(assignments))
	for _, key := range keys {
		if !replaced[key] {
			lines = append(lines, formatCredentialLine(key, assignments[key]))
		}
	}
	return writeCredentialFileLines(path, lines)
}

func formatCredentialLine(key, value string) string {
	if isBareDotEnvValue(value) {
		return key + "=" + value
	}
	line, err := godotenv.Marshal(map[string]string{key: value})
	if err != nil {
		return key + "=" + value
	}
	return line
}

func isBareDotEnvValue(value string) bool {
	if value == "" {
		return true
	}
	return !strings.ContainsAny(value, " \t\r\n#'\"\\")
}

func removeCredentialFromFile(path, key string) error {
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if k, ok := credentialLineKey(line); ok && k == key {
			continue
		}
		if k, ok := credentialClearedLineKey(line); ok && k == key {
			continue
		}
		out = append(out, line)
	}
	out = append(out, credentialClearedPrefix+key)
	return writeCredentialFileLines(path, out)
}

func readCredentialFileLines(path string) ([]string, error) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeCredentialFileLines(path string, lines []string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("credentials store unavailable")
	}
	out := ""
	if len(lines) > 0 {
		out = strings.Join(lines, "\n") + "\n"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
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
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func credentialLineKey(line string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(line), "export ")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	key, _, ok := strings.Cut(trimmed, "=")
	key = strings.TrimSpace(key)
	return key, ok && isCredentialKey(key)
}

func credentialClearedLineKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, credentialClearedPrefix) {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(trimmed, credentialClearedPrefix))
	return key, isCredentialKey(key)
}

func isCredentialKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func envFileHasValue(path, key string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	value, ok := envFileValue(path, key)
	return ok && strings.TrimSpace(value) != ""
}

func envFileHasClearedKey(path, key string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	lines, err := readCredentialFileLines(path)
	if err != nil {
		return false
	}
	for _, line := range lines {
		if k, ok := credentialClearedLineKey(line); ok && k == key {
			return true
		}
	}
	return false
}
