package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/permission"
	"reasonix/internal/sandbox"
)

func SetProjectWriteAccess(path string, allowWrite []string, permRule string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("persist write access: empty config path")
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return err
	}
	defer unlock()

	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return err
	}
	var raw []byte
	if exists {
		raw, err = fileencoding.ReadFileUTF8(resolved)
		if err != nil {
			return err
		}
	}
	body := string(raw)
	edit, err := loadForEditStrict(path, true, false)
	if err != nil {
		return err
	}

	// Replace the complete allow_write list (supports removal), normalizing each
	// entry through FormatConfigWritePath.
	home, _ := os.UserHomeDir()
	var normalized []string
	for _, dir := range allowWrite {
		formatted := sandbox.FormatConfigWritePath(strings.TrimSpace(dir), home)
		if formatted == "" {
			continue
		}
		if writeRootCovered(normalized, formatted, home) {
			continue
		}
		normalized = append(normalized, formatted)
	}

	allow := append([]string(nil), edit.Permissions.Allow...)
	if rule := strings.TrimSpace(permRule); rule != "" {
		if coveredBy := coveredPermissionRule(allow, rule); coveredBy == "" {
			allow = pruneCoveredPermissionRules(allow, rule)
			allow = append(allow, rule)
		}
	}

	if body == "" {
		body = fmt.Sprintf("[permissions]\nallow = %s\n\n[sandbox]\nallow_write = %s\n", renderStringArray(allow), renderStringArray(normalized))
	} else {
		body = upsertTOMLSectionKey(body, "permissions", "allow", "allow = "+renderStringArray(allow))
		body = upsertTOMLSectionKey(body, "sandbox", "allow_write", "allow_write = "+renderStringArray(normalized))
	}

	var candidate Config
	if _, err := toml.Decode(body, &candidate); err != nil {
		return fmt.Errorf("persist write access: validate updated config: %w", err)
	}
	if !slices.Equal(candidate.Permissions.Allow, allow) {
		return fmt.Errorf("persist write access: validate updated allow: got %v, want %v", candidate.Permissions.Allow, allow)
	}
	if !slices.Equal(candidate.Sandbox.AllowWrite, normalized) {
		return fmt.Errorf("persist write access: validate updated allow_write: got %v, want %v", candidate.Sandbox.AllowWrite, normalized)
	}
	return writeConfigFileResolved(resolved, body, configFilePerm(path))
}

// PersistProjectWriteAccess updates [permissions].allow and [sandbox].allow_write
// in one locked, validated, atomic write. permRule may be empty when ordinary
// permission is already allowed.
func PersistProjectWriteAccess(path string, dirs []string, permRule string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("persist write access: empty config path")
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return err
	}
	defer unlock()

	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return err
	}
	var raw []byte
	if exists {
		raw, err = fileencoding.ReadFileUTF8(resolved)
		if err != nil {
			return err
		}
	}
	body := string(raw)
	edit, err := loadForEditStrict(path, true, false)
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	allowWrite := append([]string(nil), edit.Sandbox.AllowWrite...)
	for _, dir := range dirs {
		formatted := sandbox.FormatConfigWritePath(dir, home)
		if formatted == "" {
			continue
		}
		if writeRootCovered(allowWrite, formatted, home) {
			continue
		}
		allowWrite = append(allowWrite, formatted)
	}

	allow := append([]string(nil), edit.Permissions.Allow...)
	if rule := strings.TrimSpace(permRule); rule != "" {
		if coveredBy := coveredPermissionRule(allow, rule); coveredBy == "" {
			allow = pruneCoveredPermissionRules(allow, rule)
			allow = append(allow, rule)
		}
	}

	if body == "" {
		body = fmt.Sprintf("[permissions]\nallow = %s\n\n[sandbox]\nallow_write = %s\n", renderStringArray(allow), renderStringArray(allowWrite))
	} else {
		body = upsertTOMLSectionKey(body, "permissions", "allow", "allow = "+renderStringArray(allow))
		body = upsertTOMLSectionKey(body, "sandbox", "allow_write", "allow_write = "+renderStringArray(allowWrite))
	}

	var candidate Config
	if _, err := toml.Decode(body, &candidate); err != nil {
		return fmt.Errorf("persist write access: validate updated config: %w", err)
	}
	if !slices.Equal(candidate.Permissions.Allow, allow) {
		return fmt.Errorf("persist write access: validate updated allow: got %v, want %v", candidate.Permissions.Allow, allow)
	}
	if !slices.Equal(candidate.Sandbox.AllowWrite, allowWrite) {
		return fmt.Errorf("persist write access: validate updated allow_write: got %v, want %v", candidate.Sandbox.AllowWrite, allowWrite)
	}
	return writeConfigFileResolved(resolved, body, configFilePerm(path))
}

func writeRootCovered(existing []string, candidate, home string) bool {
	candAbs := expandPersistedWritePath(candidate, home)
	for _, item := range existing {
		existAbs := expandPersistedWritePath(item, home)
		if existAbs == "" || candAbs == "" {
			if item == candidate {
				return true
			}
			continue
		}
		if sandbox.PathWithin(existAbs, candAbs) {
			return true
		}
	}
	return false
}

func expandPersistedWritePath(raw, home string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	abs, _, err := sandbox.NormalizeWriteDir(raw, "", home)
	if err != nil {
		if filepath.IsAbs(raw) {
			return filepath.Clean(raw)
		}
		return raw
	}
	return abs
}

// SetGlobalWriteAccess rewrites the user-global [sandbox] allow_global list in
// the user config.toml (replacement write, so removals are supported). The
// global common dirs are honored for every project/session without approval,
// including subdirectories. This is the user-config counterpart to
// SetProjectWriteAccess and backs the Settings → Permissions global-dirs panel.
func SetGlobalWriteAccess(allowGlobal []string) error {
	path := UserConfigPath()
	if path == "" {
		return fmt.Errorf("set global write access: user config path unavailable")
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return err
	}
	defer unlock()

	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return err
	}
	var raw []byte
	if exists {
		raw, err = fileencoding.ReadFileUTF8(resolved)
		if err != nil {
			return err
		}
	}
	body := string(raw)

	home, _ := os.UserHomeDir()
	var normalized []string
	for _, dir := range allowGlobal {
		formatted := sandbox.FormatConfigWritePath(strings.TrimSpace(dir), home)
		if formatted == "" {
			continue
		}
		if writeRootCovered(normalized, formatted, home) {
			continue
		}
		normalized = append(normalized, formatted)
	}

	if body == "" {
		body = "[sandbox]\nallow_global = " + renderStringArray(normalized) + "\n"
	} else {
		body = upsertTOMLSectionKey(body, "sandbox", "allow_global", "allow_global = "+renderStringArray(normalized))
	}

	var candidate Config
	if _, err := toml.Decode(body, &candidate); err != nil {
		return fmt.Errorf("set global write access: validate updated config: %w", err)
	}
	if !slices.Equal(candidate.Sandbox.AllowGlobal, normalized) {
		return fmt.Errorf("set global write access: validate updated allow_global: got %v, want %v", candidate.Sandbox.AllowGlobal, normalized)
	}
	return writeConfigFileResolved(resolved, body, configFilePerm(path))
}

func coveredPermissionRule(existing []string, candidate string) string {
	for _, item := range existing {
		if permission.RuleCoversString(item, candidate) {
			return item
		}
	}
	return ""
}

func pruneCoveredPermissionRules(existing []string, candidate string) []string {
	out := make([]string, 0, len(existing))
	for _, item := range existing {
		if permission.RuleCoversString(candidate, item) && item != candidate {
			continue
		}
		out = append(out, item)
	}
	return out
}
