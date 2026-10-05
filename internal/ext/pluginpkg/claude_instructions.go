package pluginpkg

import (
	"os"
	"path/filepath"
)

func applyClaudeCompatibility(root string, manifest *Manifest) ([]string, []CompatibilityIssue) {
	appendRootClaudeInstructions(root, manifest)
	return appendClaudeCompatibility(root, manifest)
}

func appendRootClaudeInstructions(root string, manifest *Manifest) {
	path := filepath.Join(root, claudeInstructions)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if manifest.Hooks == nil {
		manifest.Hooks = map[string][]Hook{}
	}
	manifest.Hooks["SessionStart"] = append(manifest.Hooks["SessionStart"], Hook{
		ContextFile: claudeInstructions,
		Cwd:         ".",
		Description: "Plugin CLAUDE.md startup context from " + manifest.Name,
	})
}
