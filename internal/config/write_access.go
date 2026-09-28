package config

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/permission"
	"reasonix/internal/sandbox"
)

// PersistWorkspaceWriteAccess records extra writable directories and an
// optional allow rule for workspace root under the user's Reasonix home, in
// one locked update. permRule may be empty when permission is already allowed.
func PersistWorkspaceWriteAccess(reasonixHome, root string, dirs []string, permRule string) error {
	home, _ := os.UserHomeDir()
	return NewProjectGrantStore(reasonixHome).Update(root, func(g ProjectGrant) (ProjectGrant, error) {
		for _, dir := range dirs {
			formatted := sandbox.FormatConfigWritePath(dir, home)
			if formatted != "" && !writeRootCovered(g.AllowWrite, formatted, home) {
				g.AllowWrite = append(g.AllowWrite, formatted)
			}
		}
		if rule := strings.TrimSpace(permRule); rule != "" && coveredPermissionRule(g.Allow, rule) == "" {
			g.Allow = append(pruneCoveredPermissionRules(g.Allow, rule), rule)
		}
		return g, nil
	})
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
