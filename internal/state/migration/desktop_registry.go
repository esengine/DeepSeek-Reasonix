package migration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"strings"

	"reasonix/internal/contract/config"
)

const desktopRegistryPath = "desktop/workspace-state-v1.json"

// desktopSessionOwners reads which workspace root 1.x Desktop filed each
// session under; the by-id store itself carries no workspace. An unreadable
// registry yields no owners, and those sessions fall back to the chosen workspace.
func desktopSessionOwners(tree fs.FS, home string) map[string]string {
	raw, err := fs.ReadFile(tree, path.Join(home, desktopRegistryPath))
	if err != nil {
		return nil
	}
	var state struct {
		Workspaces map[string]struct {
			Root       string   `json:"root"`
			SessionIDs []string `json:"sessionIds"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return nil
	}
	owners := map[string]string{}
	for _, ws := range state.Workspaces {
		for _, id := range ws.SessionIDs {
			owners[id] = ws.Root
		}
	}
	return owners
}

// routeToOwner sends a session to its former workspace's session directory
// while that workspace still exists, and otherwise declines.
func routeToOwner(owners map[string]string) func(string) string {
	if len(owners) == 0 {
		return nil
	}
	return func(id string) string {
		root := strings.TrimSpace(owners[id])
		if root == "" {
			return ""
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			return ""
		}
		return config.ProjectSessionDir(root)
	}
}
